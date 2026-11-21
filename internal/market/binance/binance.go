package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stockwatcher/internal/market"
)

// Popular bases are shown when the crypto search box is empty.
var Popular = []string{"BTC", "ETH", "SOL", "XRP", "BNB", "DOGE", "ADA", "AVAX", "LINK", "LTC", "BCH", "DOT", "TON", "TRX", "UNI", "NEAR", "APT", "SUI", "ATOM", "FIL"}

// Client reads Binance spot USDT markets.
type Client struct {
	HTTP *http.Client
	Base string

	mu      sync.Mutex
	pairs   []pair
	pairsAt time.Time
}

type pair struct {
	Symbol string
	Base   string
}

func (c *Client) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) base() string {
	if c != nil && c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return "https://api.binance.com"
}

// Tickers loads 24h stats for the given provider symbols (BTCUSDT).
func (c *Client) Tickers(ctx context.Context, symbols []string) (map[string]market.Quote, error) {
	out := map[string]market.Quote{}
	if len(symbols) == 0 {
		return out, nil
	}
	payload, err := json.Marshal(symbols)
	if err != nil {
		return nil, err
	}
	u := c.base() + "/api/v3/ticker/24hr?symbols=" + url.QueryEscape(string(payload))
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	return ParseTickers(body)
}

// DailyBars loads daily klines. full walks about five years; otherwise a short tail.
func (c *Client) DailyBars(ctx context.Context, symbol string, full bool) ([]market.Bar, error) {
	if !full {
		u := c.base() + "/api/v3/klines?symbol=" + url.QueryEscape(symbol) + "&interval=1d&limit=16"
		body, err := c.get(ctx, u)
		if err != nil {
			return nil, err
		}
		return ParseKlines(body)
	}
	start := time.Now().UTC().AddDate(-5, 0, 0).UnixMilli()
	var all []market.Bar
	for pages := 0; pages < 8; pages++ {
		u := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=1d&limit=1000&startTime=%d", c.base(), url.QueryEscape(symbol), start)
		body, err := c.get(ctx, u)
		if err != nil {
			return nil, err
		}
		bars, err := ParseKlines(body)
		if err != nil {
			return nil, err
		}
		if len(bars) == 0 {
			break
		}
		all = append(all, bars...)
		if len(bars) < 1000 {
			break
		}
		last, err := time.Parse("2006-01-02", bars[len(bars)-1].Day)
		if err != nil {
			break
		}
		start = last.AddDate(0, 0, 1).UnixMilli()
	}
	return all, nil
}

// Search filters cached USDT spot markets. An empty query returns popular bases.
func (c *Client) Search(ctx context.Context, q string) ([]market.Hit, error) {
	pairs, err := c.catalog(ctx)
	if err != nil {
		return nil, err
	}
	q = strings.ToUpper(strings.TrimSpace(q))
	if q == "" {
		byBase := map[string]pair{}
		for _, p := range pairs {
			byBase[p.Base] = p
		}
		out := make([]market.Hit, 0, len(Popular))
		for _, base := range Popular {
			p, ok := byBase[base]
			if !ok {
				continue
			}
			out = append(out, hit(p))
		}
		return out, nil
	}
	type ranked struct {
		p     pair
		score int
	}
	var rows []ranked
	for _, p := range pairs {
		score := -1
		switch {
		case p.Base == q:
			score = 0
		case strings.HasPrefix(p.Base, q):
			score = 1
		case strings.Contains(p.Base, q):
			score = 2
		}
		if score >= 0 {
			rows = append(rows, ranked{p: p, score: score})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score < rows[j].score
		}
		return rows[i].p.Base < rows[j].p.Base
	})
	if len(rows) > 20 {
		rows = rows[:20]
	}
	out := make([]market.Hit, 0, len(rows))
	for _, row := range rows {
		out = append(out, hit(row.p))
	}
	return out, nil
}

func hit(p pair) market.Hit {
	return market.Hit{
		AssetClass:     market.Crypto,
		Display:        p.Base,
		ProviderSymbol: p.Symbol,
		Name:           p.Base + " / USDT",
	}
}

func (c *Client) catalog(ctx context.Context) ([]pair, error) {
	c.mu.Lock()
	if len(c.pairs) > 0 && time.Since(c.pairsAt) < 12*time.Hour {
		pairs := c.pairs
		c.mu.Unlock()
		return pairs, nil
	}
	c.mu.Unlock()
	body, err := c.get(ctx, c.base()+"/api/v3/exchangeInfo")
	if err != nil {
		return nil, err
	}
	pairs, err := ParseExchangeInfo(body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.pairs = pairs
	c.pairsAt = time.Now()
	c.mu.Unlock()
	return pairs, nil
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "stockwatcher/1.0")
	res, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == 418 {
		return nil, fmt.Errorf("binance rate limit")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance status %d", res.StatusCode)
	}
	return body, nil
}

// ParseTickers decodes a 24hr ticker array.
func ParseTickers(data []byte) (map[string]market.Quote, error) {
	var rows []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		PrevClosePrice     string `json:"prevClosePrice"`
		PriceChangePercent string `json:"priceChangePercent"`
		CloseTime          int64  `json:"closeTime"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		var apiErr struct {
			Msg string `json:"msg"`
		}
		if jerr := json.Unmarshal(data, &apiErr); jerr == nil && apiErr.Msg != "" {
			return nil, fmt.Errorf("binance: %s", apiErr.Msg)
		}
		return nil, err
	}
	out := make(map[string]market.Quote, len(rows))
	for _, row := range rows {
		price, err := strconv.ParseFloat(row.LastPrice, 64)
		if err != nil || price == 0 {
			continue
		}
		prev, _ := strconv.ParseFloat(row.PrevClosePrice, 64)
		pct, err := strconv.ParseFloat(row.PriceChangePercent, 64)
		var day *float64
		if err == nil {
			day = &pct
		}
		asOf := time.Now()
		if row.CloseTime > 0 {
			asOf = time.UnixMilli(row.CloseTime)
		}
		out[row.Symbol] = market.Quote{Price: price, PrevClose: prev, DayPct: day, AsOf: asOf}
	}
	return out, nil
}

// ParseKlines decodes spot klines. Open time is the UTC session date.
func ParseKlines(data []byte) ([]market.Bar, error) {
	var rows [][]any
	if err := json.Unmarshal(data, &rows); err != nil {
		var apiErr struct {
			Msg string `json:"msg"`
		}
		if jerr := json.Unmarshal(data, &apiErr); jerr == nil && apiErr.Msg != "" {
			return nil, fmt.Errorf("binance: %s", apiErr.Msg)
		}
		return nil, err
	}
	bars := make([]market.Bar, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		openMS, ok := asInt64(row[0])
		if !ok {
			continue
		}
		op, ok1 := asFloat(row[1])
		hi, ok2 := asFloat(row[2])
		lo, ok3 := asFloat(row[3])
		cl, ok4 := asFloat(row[4])
		vol, ok5 := asFloat(row[5])
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || cl == 0 {
			continue
		}
		bars = append(bars, market.Bar{
			Day:    market.UTCDay(openMS),
			Open:   op,
			High:   hi,
			Low:    lo,
			Close:  cl,
			Volume: vol,
		})
	}
	return bars, nil
}

// ParseExchangeInfo keeps TRADING USDT spot symbols.
func ParseExchangeInfo(data []byte) ([]pair, error) {
	var raw struct {
		Symbols []struct {
			Symbol               string `json:"symbol"`
			Status               string `json:"status"`
			BaseAsset            string `json:"baseAsset"`
			QuoteAsset           string `json:"quoteAsset"`
			IsSpotTradingAllowed bool   `json:"isSpotTradingAllowed"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]pair, 0, len(raw.Symbols))
	for _, s := range raw.Symbols {
		if s.Status != "TRADING" || s.QuoteAsset != "USDT" || !s.IsSpotTradingAllowed || s.BaseAsset == "" {
			continue
		}
		out = append(out, pair{Symbol: s.Symbol, Base: s.BaseAsset})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out, nil
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}
