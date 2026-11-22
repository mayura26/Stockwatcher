package yahoo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"stockwatcher/internal/market"
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

const quoteSummaryModules = "price,summaryDetail,financialData,defaultKeyStatistics,calendarEvents"

// Client reads Yahoo's chart, search, and quote-summary endpoints. Charts cover
// CME futures, daily bars, and listings Finnhub does not quote. Search is the
// fallback when a US stock query has no hits. Quote summary fills a company
// fact Finnhub did not return.
type Client struct {
	HTTP *http.Client
	Base string

	mu   sync.Mutex
	last time.Time

	crumbMu sync.Mutex
	crumb   string
	jarOnce sync.Once
	jarHTTP *http.Client
	jarErr  error
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return "https://query1.finance.yahoo.com"
}

// Chart loads a range such as "5d" or "5y". adjust scales OHLC by adjclose/close.
func (c *Client) Chart(ctx context.Context, symbol, rng string, adjust bool) (market.Quote, []market.Bar, error) {
	c.pace()
	u := c.base() + "/v8/finance/chart/" + url.PathEscape(symbol) + "?range=" + url.QueryEscape(rng) + "&interval=1d&events=div%2Csplit"
	body, err := c.fetch(ctx, symbol, u)
	if err != nil {
		return market.Quote{}, nil, err
	}
	return ParseChart(body, adjust)
}

// Intraday loads about five days of unadjusted 5-minute closes.
// Point time is the bar close, so it lines up with the live price.
// prepost asks Yahoo for premarket and after-hours bars. The returned points
// stay inside the regular session, and Extended is that extra print versus
// the regular price. Futures leave prepost false so the overnight session
// stays in the series.
func (c *Client) Intraday(ctx context.Context, symbol string, prepost bool) ([]market.Point, Extended, error) {
	c.pace()
	u := c.base() + "/v8/finance/chart/" + url.PathEscape(symbol) + "?range=5d&interval=5m"
	if prepost {
		u += "&includePrePost=true"
	}
	body, err := c.fetch(ctx, symbol, u)
	if err != nil {
		return nil, Extended{}, err
	}
	return ParseIntraday(body)
}

// Candles loads unadjusted intraday OHLC. interval is 15m or 60m. rng is a Yahoo range such as 5d or 60d.
func (c *Client) Candles(ctx context.Context, symbol, interval, rng string) ([]market.IntradayBar, error) {
	c.pace()
	u := c.base() + "/v8/finance/chart/" + url.PathEscape(symbol) + "?range=" + url.QueryEscape(rng) + "&interval=" + url.QueryEscape(interval)
	body, err := c.fetch(ctx, symbol, u)
	if err != nil {
		return nil, err
	}
	return ParseCandles(body)
}

// CandlesSince loads unadjusted intraday OHLC from from through to.
// from is inclusive so the forming bar already stored can be replaced.
func (c *Client) CandlesSince(ctx context.Context, symbol, interval string, from, to time.Time) ([]market.IntradayBar, error) {
	if to.Unix() <= from.Unix() {
		to = from.Add(time.Second)
	}
	c.pace()
	u := fmt.Sprintf("%s/v8/finance/chart/%s?period1=%d&period2=%d&interval=%s",
		c.base(), url.PathEscape(symbol), from.Unix(), to.Unix(), url.QueryEscape(interval))
	body, err := c.fetch(ctx, symbol, u)
	if err != nil {
		return nil, err
	}
	return ParseCandles(body)
}

func (c *Client) fetch(ctx context.Context, symbol, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("yahoo rate limit for %s", symbol)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo status %d for %s", res.StatusCode, symbol)
	}
	return body, nil
}

// DailyBars loads full history (about 5 years) or a short tail.
func (c *Client) DailyBars(ctx context.Context, symbol string, adjust, full bool) ([]market.Bar, error) {
	rng := "10d"
	if full {
		rng = "5y"
	}
	_, bars, err := c.Chart(ctx, symbol, rng, adjust)
	return bars, err
}

// Search looks up equities and ETFs. An empty query returns no hits.
func (c *Client) Search(ctx context.Context, q string) ([]market.Hit, error) {
	if c == nil {
		return nil, fmt.Errorf("yahoo unavailable")
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	body, err := c.searchOnce(ctx, q)
	if err != nil && strings.Contains(err.Error(), "rate limit") {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(700 * time.Millisecond):
		}
		body, err = c.searchOnce(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	return ParseSearch(body)
}

func (c *Client) searchOnce(ctx context.Context, q string) ([]byte, error) {
	c.pace()
	u := c.base() + "/v1/finance/search?q=" + url.QueryEscape(q) + "&quotesCount=20&newsCount=0"
	return c.fetch(ctx, q, u)
}

// Facts loads market cap, trailing P/E, TTM revenue, TTM net income, and the
// next earnings date. hour is always empty: Yahoo does not say open or close.
// A missing figure comes back nil. Cap and P/E are kept only when positive.
func (c *Client) Facts(ctx context.Context, symbol string, now time.Time) (marketCap, pe, revenue, earnings *float64, date, hour string, err error) {
	if c == nil {
		return nil, nil, nil, nil, "", "", fmt.Errorf("yahoo unavailable")
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, nil, nil, nil, "", "", fmt.Errorf("empty yahoo symbol")
	}
	body, err := c.quoteSummary(ctx, symbol)
	if err != nil {
		return nil, nil, nil, nil, "", "", err
	}
	marketCap, pe, revenue, earnings, date, err = ParseFacts(body, market.NYDay(now.Unix()))
	return marketCap, pe, revenue, earnings, date, "", err
}

func (c *Client) quoteSummary(ctx context.Context, symbol string) ([]byte, error) {
	path := "/v10/finance/quoteSummary/" + url.PathEscape(symbol) + "?modules=" + quoteSummaryModules
	if c.Base != "" {
		c.pace()
		return c.fetch(ctx, symbol, c.base()+path)
	}
	body, status, err := c.quoteSummaryLive(ctx, path)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		c.forgetCrumb()
		body, status, err = c.quoteSummaryLive(ctx, path)
		if err != nil {
			return nil, err
		}
	}
	if status == http.StatusTooManyRequests {
		return nil, fmt.Errorf("yahoo rate limit for %s", symbol)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("yahoo status %d for %s", status, symbol)
	}
	return body, nil
}

func (c *Client) quoteSummaryLive(ctx context.Context, path string) ([]byte, int, error) {
	crumb, err := c.ensureCrumb(ctx)
	if err != nil {
		return nil, 0, err
	}
	u := "https://query1.finance.yahoo.com" + path + "&crumb=" + url.QueryEscape(crumb)
	return c.doJar(ctx, u, "application/json")
}

func (c *Client) ensureCrumb(ctx context.Context) (string, error) {
	c.crumbMu.Lock()
	defer c.crumbMu.Unlock()
	if c.crumb != "" {
		return c.crumb, nil
	}
	if _, _, err := c.doJar(ctx, "https://fc.yahoo.com", "*/*"); err != nil {
		return "", err
	}
	// getcrumb is text/plain. Accept: application/json is a 406.
	body, status, err := c.doJar(ctx, "https://query1.finance.yahoo.com/v1/test/getcrumb", "*/*")
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("yahoo crumb status %d", status)
	}
	crumb := strings.TrimSpace(string(body))
	if strings.HasPrefix(crumb, `"`) {
		if s, err := strconv.Unquote(crumb); err == nil {
			crumb = s
		}
	}
	if crumb == "" || strings.ContainsAny(crumb, "<{}\n ") {
		return "", fmt.Errorf("yahoo crumb missing")
	}
	c.crumb = crumb
	return crumb, nil
}

func (c *Client) forgetCrumb() {
	c.crumbMu.Lock()
	c.crumb = ""
	c.crumbMu.Unlock()
}

func (c *Client) jarClient() (*http.Client, error) {
	c.jarOnce.Do(func() {
		jar, err := cookiejar.New(nil)
		if err != nil {
			c.jarErr = err
			return
		}
		c.jarHTTP = &http.Client{Timeout: 20 * time.Second, Jar: jar}
	})
	if c.jarErr != nil {
		return nil, c.jarErr
	}
	return c.jarHTTP, nil
}

func (c *Client) doJar(ctx context.Context, u, accept string) ([]byte, int, error) {
	c.pace()
	client, err := c.jarClient()
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", browserUA)
	if accept == "" {
		accept = "*/*"
	}
	req.Header.Set("Accept", accept)
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	return body, res.StatusCode, nil
}

// ParseSearch keeps equities and ETFs. The symbol is the Yahoo listing, such as 005930.KS.
// Names are cut to the length the API will store.
func ParseSearch(data []byte) ([]market.Hit, error) {
	var raw struct {
		Quotes []struct {
			Symbol    string `json:"symbol"`
			Shortname string `json:"shortname"`
			Longname  string `json:"longname"`
			QuoteType string `json:"quoteType"`
			ExchDisp  string `json:"exchDisp"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]market.Hit, 0, 20)
	seen := map[string]bool{}
	for _, r := range raw.Quotes {
		switch strings.ToUpper(strings.TrimSpace(r.QuoteType)) {
		case "EQUITY", "ETF":
		default:
			continue
		}
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" || len(sym) > 16 || seen[sym] {
			continue
		}
		seen[sym] = true
		name := strings.TrimSpace(r.Longname)
		if name == "" {
			name = strings.TrimSpace(r.Shortname)
		}
		out = append(out, market.Hit{
			AssetClass:     market.Stock,
			Display:        sym,
			ProviderSymbol: sym,
			Name:           searchName(name, r.ExchDisp),
		})
		if len(out) >= 20 {
			break
		}
	}
	return out, nil
}

func searchName(company, exch string) string {
	company = strings.TrimSpace(company)
	exch = strings.TrimSpace(exch)
	const max = 80
	if exch == "" {
		return trimRunes(company, max)
	}
	suffix := " · " + exch
	room := max - len([]rune(suffix))
	if room < 1 {
		return trimRunes(exch, max)
	}
	company = trimRunes(company, room)
	if company == "" {
		return trimRunes(exch, max)
	}
	return company + suffix
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := 300*time.Millisecond - time.Since(c.last)
	if wait > 0 && !c.last.IsZero() {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

// ParseChart decodes a Yahoo v8 chart payload.
func ParseChart(data []byte, adjust bool) (market.Quote, []market.Bar, error) {
	var env struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice         float64  `json:"regularMarketPrice"`
					RegularMarketTime          int64    `json:"regularMarketTime"`
					RegularMarketChangePercent *float64 `json:"regularMarketChangePercent"`
					PreviousClose              *float64 `json:"previousClose"`
					ChartPreviousClose         float64  `json:"chartPreviousClose"`
					ShortName                  string   `json:"shortName"`
					Symbol                     string   `json:"symbol"`
				} `json:"meta"`
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []*float64 `json:"open"`
						High   []*float64 `json:"high"`
						Low    []*float64 `json:"low"`
						Close  []*float64 `json:"close"`
						Volume []*float64 `json:"volume"`
					} `json:"quote"`
					Adjclose []struct {
						Adjclose []*float64 `json:"adjclose"`
					} `json:"adjclose"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return market.Quote{}, nil, err
	}
	if env.Chart.Error != nil {
		msg := env.Chart.Error.Description
		if msg == "" {
			msg = env.Chart.Error.Code
		}
		if msg == "" {
			msg = "yahoo chart error"
		}
		return market.Quote{}, nil, fmt.Errorf("%s", msg)
	}
	if len(env.Chart.Result) == 0 {
		return market.Quote{}, nil, fmt.Errorf("yahoo returned no chart")
	}
	res := env.Chart.Result[0]
	var quoteSeries struct {
		Open   []*float64
		High   []*float64
		Low    []*float64
		Close  []*float64
		Volume []*float64
	}
	if len(res.Indicators.Quote) > 0 {
		q := res.Indicators.Quote[0]
		quoteSeries.Open = q.Open
		quoteSeries.High = q.High
		quoteSeries.Low = q.Low
		quoteSeries.Close = q.Close
		quoteSeries.Volume = q.Volume
	}
	var adj []*float64
	if len(res.Indicators.Adjclose) > 0 {
		adj = res.Indicators.Adjclose[0].Adjclose
	}
	bars := make([]market.Bar, 0, len(res.Timestamp))
	for i, ts := range res.Timestamp {
		cl := at(quoteSeries.Close, i)
		if cl == nil {
			continue
		}
		op := or(at(quoteSeries.Open, i), *cl)
		hi := or(at(quoteSeries.High, i), *cl)
		lo := or(at(quoteSeries.Low, i), *cl)
		vol := 0.0
		if v := at(quoteSeries.Volume, i); v != nil {
			vol = *v
		}
		closePx := *cl
		if adjust {
			if a := at(adj, i); a != nil {
				op, hi, lo, closePx = market.ScaleOHLC(op, hi, lo, closePx, *a)
			}
		}
		bars = append(bars, market.Bar{
			Day:    market.NYDay(ts),
			Open:   op,
			High:   hi,
			Low:    lo,
			Close:  closePx,
			Volume: vol,
		})
	}
	if res.Meta.RegularMarketPrice == 0 {
		return market.Quote{}, bars, fmt.Errorf("yahoo quote missing for %s", res.Meta.Symbol)
	}
	prev := 0.0
	if res.Meta.PreviousClose != nil && *res.Meta.PreviousClose > 0 {
		prev = *res.Meta.PreviousClose
	} else if len(bars) >= 2 {
		prev = bars[len(bars)-2].Close
	} else if res.Meta.ChartPreviousClose > 0 {
		prev = res.Meta.ChartPreviousClose
	}
	asOf := time.Now()
	if res.Meta.RegularMarketTime > 0 {
		asOf = time.Unix(res.Meta.RegularMarketTime, 0)
	}
	return market.Quote{
		Price:     res.Meta.RegularMarketPrice,
		PrevClose: prev,
		DayPct:    res.Meta.RegularMarketChangePercent,
		AsOf:      asOf,
	}, bars, nil
}

const intradayStep = 5 * time.Minute

// Extended is a premarket or after-hours percent versus the regular price.
// Session is "pre" or "post". An empty Session means nothing to show.
type Extended struct {
	Session string
	Pct     *float64
}

type sessionWindow struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// ParseIntraday decodes a Yahoo v8 chart of 5-minute bars into close prints.
// When the payload includes premarket and after-hours windows, those bars are
// left out of the points (so the sparkline stays the regular session) and the
// newest of them becomes Extended, versus regularMarketPrice. A later regular
// bar clears it. A flat 0% still counts.
func ParseIntraday(data []byte) ([]market.Point, Extended, error) {
	var env struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice float64         `json:"regularMarketPrice"`
					TradingPeriods     json.RawMessage `json:"tradingPeriods"`
				} `json:"meta"`
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, Extended{}, err
	}
	if env.Chart.Error != nil {
		msg := env.Chart.Error.Description
		if msg == "" {
			msg = env.Chart.Error.Code
		}
		if msg == "" {
			msg = "yahoo chart error"
		}
		return nil, Extended{}, fmt.Errorf("%s", msg)
	}
	if len(env.Chart.Result) == 0 {
		return nil, Extended{}, fmt.Errorf("yahoo returned no chart")
	}
	res := env.Chart.Result[0]
	var closes []*float64
	if len(res.Indicators.Quote) > 0 {
		closes = res.Indicators.Quote[0].Close
	}
	preW, regW, postW := splitSessions(res.Meta.TradingPeriods)
	filterRegular := len(regW) > 0 && (len(preW) > 0 || len(postW) > 0)
	points := make([]market.Point, 0, len(res.Timestamp))
	var lastReg, bestTs int64
	var bestPx float64
	var bestSession string
	for i, ts := range res.Timestamp {
		cl := at(closes, i)
		if cl == nil || *cl == 0 {
			continue
		}
		kind := ""
		switch {
		case inWindow(ts, regW):
			kind = "regular"
		case inWindow(ts, postW):
			kind = "post"
		case inWindow(ts, preW):
			kind = "pre"
		}
		if !filterRegular || kind == "regular" {
			points = append(points, market.Point{Time: barStart(ts).Add(intradayStep), Price: *cl})
		}
		if kind == "regular" && ts >= lastReg {
			lastReg = ts
		}
		if (kind == "pre" || kind == "post") && ts >= bestTs {
			bestTs = ts
			bestPx = *cl
			bestSession = kind
		}
	}
	return points, extendedMove(res.Meta.RegularMarketPrice, lastReg, bestTs, bestPx, bestSession), nil
}

func splitSessions(raw json.RawMessage) (pre, regular, post []sessionWindow) {
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var keyed struct {
		Pre     [][]sessionWindow `json:"pre"`
		Regular [][]sessionWindow `json:"regular"`
		Post    [][]sessionWindow `json:"post"`
	}
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return
	}
	return flattenWindows(keyed.Pre), flattenWindows(keyed.Regular), flattenWindows(keyed.Post)
}

func flattenWindows(days [][]sessionWindow) []sessionWindow {
	var out []sessionWindow
	for _, day := range days {
		out = append(out, day...)
	}
	return out
}

func inWindow(ts int64, windows []sessionWindow) bool {
	for _, w := range windows {
		if ts >= w.Start && ts <= w.End {
			return true
		}
	}
	return false
}

func extendedMove(regular float64, lastReg, bestTs int64, bestPx float64, session string) Extended {
	if session == "" || regular <= 0 || bestPx <= 0 || bestTs <= lastReg {
		return Extended{}
	}
	v := (bestPx/regular - 1) * 100
	return Extended{Session: session, Pct: &v}
}

// ParseCandles decodes a Yahoo intraday chart into OHLC. Time is the bar open.
func ParseCandles(data []byte) ([]market.IntradayBar, error) {
	var env struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []*float64 `json:"open"`
						High   []*float64 `json:"high"`
						Low    []*float64 `json:"low"`
						Close  []*float64 `json:"close"`
						Volume []*float64 `json:"volume"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	if env.Chart.Error != nil {
		msg := env.Chart.Error.Description
		if msg == "" {
			msg = env.Chart.Error.Code
		}
		if msg == "" {
			msg = "yahoo chart error"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if len(env.Chart.Result) == 0 {
		return nil, fmt.Errorf("yahoo returned no chart")
	}
	res := env.Chart.Result[0]
	var quote struct {
		Open, High, Low, Close, Volume []*float64
	}
	if len(res.Indicators.Quote) > 0 {
		q := res.Indicators.Quote[0]
		quote.Open, quote.High, quote.Low, quote.Close, quote.Volume = q.Open, q.High, q.Low, q.Close, q.Volume
	}
	out := make([]market.IntradayBar, 0, len(res.Timestamp))
	for i, ts := range res.Timestamp {
		cl := at(quote.Close, i)
		if cl == nil || *cl == 0 {
			continue
		}
		vol := 0.0
		if v := at(quote.Volume, i); v != nil {
			vol = *v
		}
		out = append(out, market.IntradayBar{
			OpenUnix: barStart(ts).Unix(),
			Open:     or(at(quote.Open, i), *cl),
			High:     or(at(quote.High, i), *cl),
			Low:      or(at(quote.Low, i), *cl),
			Close:    *cl,
			Volume:   vol,
		})
	}
	return out, nil
}

func barStart(ts int64) time.Time {
	if ts > 1e12 {
		return time.UnixMilli(ts)
	}
	return time.Unix(ts, 0)
}

func at(s []*float64, i int) *float64 {
	if i < 0 || i >= len(s) {
		return nil
	}
	return s[i]
}

func or(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

type yahooValue struct {
	raw *float64
	fmt string
}

func (v *yahooValue) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '{' {
		var obj struct {
			Raw *float64 `json:"raw"`
			Fmt string   `json:"fmt"`
		}
		if err := json.Unmarshal(b, &obj); err != nil {
			return err
		}
		v.raw = obj.Raw
		v.fmt = obj.Fmt
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	v.raw = &n
	return nil
}

type yahooWhenList []yahooValue

func (l *yahooWhenList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '[' {
		var arr []yahooValue
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*l = arr
		return nil
	}
	var one yahooValue
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*l = yahooWhenList{one}
	return nil
}

// ParseFacts decodes a quoteSummary payload. today is YYYY-MM-DD. The earnings
// date is the earliest release on or after today. A fmt of YYYY-MM-DD wins;
// otherwise the UTC day of the unix raw value is used.
func ParseFacts(data []byte, today string) (marketCap, pe, revenue, earnings *float64, date string, err error) {
	var raw struct {
		QuoteSummary struct {
			Result []struct {
				Price struct {
					MarketCap *yahooValue `json:"marketCap"`
				} `json:"price"`
				SummaryDetail struct {
					MarketCap  *yahooValue `json:"marketCap"`
					TrailingPE *yahooValue `json:"trailingPE"`
				} `json:"summaryDetail"`
				FinancialData struct {
					TotalRevenue *yahooValue `json:"totalRevenue"`
				} `json:"financialData"`
				DefaultKeyStatistics struct {
					TrailingPE        *yahooValue `json:"trailingPE"`
					NetIncomeToCommon *yahooValue `json:"netIncomeToCommon"`
				} `json:"defaultKeyStatistics"`
				CalendarEvents struct {
					Earnings struct {
						EarningsDate yahooWhenList `json:"earningsDate"`
					} `json:"earnings"`
				} `json:"calendarEvents"`
			} `json:"result"`
			Error *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"quoteSummary"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, nil, nil, "", err
	}
	if raw.QuoteSummary.Error != nil {
		msg := strings.TrimSpace(raw.QuoteSummary.Error.Description)
		if msg == "" {
			msg = strings.TrimSpace(raw.QuoteSummary.Error.Code)
		}
		if msg == "" {
			msg = "yahoo quote summary"
		}
		return nil, nil, nil, nil, "", errors.New(msg)
	}
	if len(raw.QuoteSummary.Result) == 0 {
		return nil, nil, nil, nil, "", nil
	}
	row := raw.QuoteSummary.Result[0]
	return firstPositive(row.Price.MarketCap, row.SummaryDetail.MarketCap),
		firstPositive(row.SummaryDetail.TrailingPE, row.DefaultKeyStatistics.TrailingPE),
		yahooFinite(row.FinancialData.TotalRevenue),
		yahooFinite(row.DefaultKeyStatistics.NetIncomeToCommon),
		nextEarningsDate(row.CalendarEvents.Earnings.EarningsDate, today),
		nil
}

func firstPositive(vals ...*yahooValue) *float64 {
	for _, v := range vals {
		if n := yahooPositive(v); n != nil {
			return n
		}
	}
	return nil
}

func yahooPositive(v *yahooValue) *float64 {
	n := yahooFinite(v)
	if n == nil || *n <= 0 {
		return nil
	}
	return n
}

func yahooFinite(v *yahooValue) *float64 {
	if v == nil || v.raw == nil || math.IsNaN(*v.raw) || math.IsInf(*v.raw, 0) {
		return nil
	}
	n := *v.raw
	return &n
}

func nextEarningsDate(dates []yahooValue, today string) string {
	best := ""
	for _, d := range dates {
		day := yahooCalendarDay(d.fmt, d.raw)
		if day == "" || (today != "" && day < today) {
			continue
		}
		if best == "" || day < best {
			best = day
		}
	}
	return best
}

func yahooCalendarDay(fmtStr string, raw *float64) string {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(fmtStr)); err == nil {
		return t.Format("2006-01-02")
	}
	if raw == nil || math.IsNaN(*raw) || math.IsInf(*raw, 0) {
		return ""
	}
	sec := int64(*raw)
	if sec > 1e12 {
		sec = sec / 1000
	}
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format("2006-01-02")
}
