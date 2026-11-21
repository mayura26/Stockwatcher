package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"stockwatcher/internal/market"
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Client reads Yahoo's chart endpoint. It is used for CME futures quotes and
// for daily bars (futures, and US stocks when Finnhub candles are unavailable).
type Client struct {
	HTTP *http.Client
	Base string

	mu   sync.Mutex
	last time.Time
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

// Candles loads unadjusted intraday OHLC. interval is 5m or 60m. rng is a Yahoo range such as 60d or 2y.
func (c *Client) Candles(ctx context.Context, symbol, interval, rng string) ([]market.IntradayBar, error) {
	c.pace()
	u := c.base() + "/v8/finance/chart/" + url.PathEscape(symbol) + "?range=" + url.QueryEscape(rng) + "&interval=" + url.QueryEscape(interval)
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
						Open  []*float64 `json:"open"`
						High  []*float64 `json:"high"`
						Low   []*float64 `json:"low"`
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
		Open, High, Low, Close []*float64
	}
	if len(res.Indicators.Quote) > 0 {
		q := res.Indicators.Quote[0]
		quote.Open, quote.High, quote.Low, quote.Close = q.Open, q.High, q.Low, q.Close
	}
	out := make([]market.IntradayBar, 0, len(res.Timestamp))
	for i, ts := range res.Timestamp {
		cl := at(quote.Close, i)
		if cl == nil || *cl == 0 {
			continue
		}
		out = append(out, market.IntradayBar{
			OpenUnix: barStart(ts).Unix(),
			Open:     or(at(quote.Open, i), *cl),
			High:     or(at(quote.High, i), *cl),
			Low:      or(at(quote.Low, i), *cl),
			Close:    *cl,
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
