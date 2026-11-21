package finnhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"stockwatcher/internal/market"
)

// ErrNoKey is returned when stock search or quotes are requested without a key.
var ErrNoKey = errors.New("FINNHUB_API_KEY is not set")

// Client calls Finnhub for US stock quotes and symbol search.
// Daily candles are probed once; on the free plan that route is paid and stays off.
type Client struct {
	Key  string
	HTTP *http.Client
	Base string
}

func (c *Client) Enabled() bool {
	return c != nil && c.Key != ""
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
	return "https://finnhub.io/api/v1"
}

// Quote loads the latest US stock price.
func (c *Client) Quote(ctx context.Context, symbol string) (market.Quote, error) {
	if !c.Enabled() {
		return market.Quote{}, ErrNoKey
	}
	body, err := c.get(ctx, "/quote?symbol="+url.QueryEscape(symbol))
	if err != nil {
		return market.Quote{}, err
	}
	return ParseQuote(body)
}

// Search returns US common stocks. Symbols with an exchange suffix are dropped.
func (c *Client) Search(ctx context.Context, q string) ([]market.Hit, error) {
	if !c.Enabled() {
		return nil, ErrNoKey
	}
	body, err := c.get(ctx, "/search?q="+url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	return ParseSearch(body)
}

// ProbeCandles reports whether /stock/candle is authorized for this key.
// A 429 or 5xx is an error so the caller can retry later instead of caching a denial.
func (c *Client) ProbeCandles(ctx context.Context) (bool, error) {
	if !c.Enabled() {
		return false, nil
	}
	now := time.Now().UTC()
	path := fmt.Sprintf("/stock/candle?symbol=AAPL&resolution=D&from=%d&to=%d", now.AddDate(0, 0, -10).Unix(), now.Unix())
	status, body, err := c.getStatus(ctx, path)
	if err != nil {
		return false, err
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		return false, fmt.Errorf("finnhub probe status %d", status)
	}
	return CandleAllowed(status, body), nil
}

// DailyBars loads daily candles. full is about five years; otherwise a short tail.
func (c *Client) DailyBars(ctx context.Context, symbol string, full bool) ([]market.Bar, error) {
	if !c.Enabled() {
		return nil, ErrNoKey
	}
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -15)
	if full {
		from = now.AddDate(-5, 0, 0)
	}
	path := fmt.Sprintf("/stock/candle?symbol=%s&resolution=D&from=%d&to=%d", url.QueryEscape(symbol), from.Unix(), now.Unix())
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	return ParseCandles(body)
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	status, body, err := c.getStatus(ctx, path)
	if err != nil {
		return nil, err
	}
	if status == http.StatusTooManyRequests {
		return nil, fmt.Errorf("finnhub rate limit")
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("finnhub status %d", status)
	}
	return body, nil
}

func (c *Client) getStatus(ctx context.Context, path string) (int, []byte, error) {
	u := c.base() + path
	if strings.Contains(u, "?") {
		u += "&token=" + url.QueryEscape(c.Key)
	} else {
		u += "?token=" + url.QueryEscape(c.Key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "stockwatcher/1.0")
	res, err := c.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, body, nil
}

// ParseQuote decodes /quote.
func ParseQuote(data []byte) (market.Quote, error) {
	var raw struct {
		C  float64 `json:"c"`
		PC float64 `json:"pc"`
		DP float64 `json:"dp"`
		T  int64   `json:"t"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return market.Quote{}, err
	}
	if raw.C == 0 {
		return market.Quote{}, fmt.Errorf("finnhub returned no price")
	}
	asOf := time.Now()
	if raw.T > 0 {
		asOf = time.Unix(raw.T, 0)
	}
	dp := raw.DP
	return market.Quote{Price: raw.C, PrevClose: raw.PC, DayPct: &dp, AsOf: asOf}, nil
}

// ParseSearch keeps US common stocks (no dot or colon in the symbol).
func ParseSearch(data []byte) ([]market.Hit, error) {
	var raw struct {
		Result []struct {
			Description   string `json:"description"`
			DisplaySymbol string `json:"displaySymbol"`
			Symbol        string `json:"symbol"`
			Type          string `json:"type"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]market.Hit, 0, 20)
	seen := map[string]bool{}
	for _, r := range raw.Result {
		if r.Type != "Common Stock" {
			continue
		}
		sym := r.DisplaySymbol
		if sym == "" {
			sym = r.Symbol
		}
		sym = strings.ToUpper(sym)
		if strings.ContainsAny(sym, ".:") || sym == "" || seen[sym] {
			continue
		}
		seen[sym] = true
		out = append(out, market.Hit{
			AssetClass:     market.Stock,
			Display:        sym,
			ProviderSymbol: sym,
			Name:           r.Description,
		})
		if len(out) >= 20 {
			break
		}
	}
	return out, nil
}

// ParseCandles decodes /stock/candle.
func ParseCandles(data []byte) ([]market.Bar, error) {
	var raw struct {
		C     []float64 `json:"c"`
		H     []float64 `json:"h"`
		L     []float64 `json:"l"`
		O     []float64 `json:"o"`
		T     []int64   `json:"t"`
		V     []float64 `json:"v"`
		S     string    `json:"s"`
		Error string    `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Error != "" {
		return nil, errors.New(raw.Error)
	}
	if raw.S != "ok" {
		return nil, fmt.Errorf("finnhub candles %s", raw.S)
	}
	n := len(raw.T)
	bars := make([]market.Bar, 0, n)
	for i := 0; i < n; i++ {
		if i >= len(raw.O) || i >= len(raw.H) || i >= len(raw.L) || i >= len(raw.C) {
			break
		}
		vol := 0.0
		if i < len(raw.V) {
			vol = raw.V[i]
		}
		bars = append(bars, market.Bar{
			Day:    market.NYDay(raw.T[i]),
			Open:   raw.O[i],
			High:   raw.H[i],
			Low:    raw.L[i],
			Close:  raw.C[i],
			Volume: vol,
		})
	}
	return bars, nil
}

// CandleAllowed reports whether a probe response means the candle route is on the plan.
func CandleAllowed(status int, body []byte) bool {
	if status != http.StatusOK {
		return false
	}
	var raw struct {
		S     string `json:"s"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	if raw.Error != "" {
		return false
	}
	return raw.S == "ok" || raw.S == "no_data"
}
