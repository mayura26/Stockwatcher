package market

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Stock  = "stock"
	Future = "future"
	Crypto = "crypto"
)

// Quote is a last price from a provider. DayPct is set when the provider's
// own day (or 24h) percent should be stored as-is.
// IntradaySet means this refresh loaded 5-minute closes into Intraday.
// A failed load leaves IntradaySet false so stored 1H and 4H stay put.
type Quote struct {
	Price       float64
	PrevClose   float64
	DayPct      *float64
	AsOf        time.Time
	Intraday    []Point
	IntradaySet bool
}

// Point is one intraday print. Time is when that price was printed.
type Point struct {
	Time  time.Time
	Price float64
}

// Bar is one daily session.
type Bar struct {
	Day             string
	Open, High, Low float64
	Close, Volume   float64
}

// Candle is a Lightweight Charts business-day bar. Time is the bucket start
// so the current week or month updates in place.
type Candle struct {
	Time  string  `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

// Hit is a search result the client can add to a group.
type Hit struct {
	AssetClass     string `json:"asset_class"`
	Display        string `json:"display"`
	ProviderSymbol string `json:"provider_symbol"`
	Name           string `json:"name"`
}

// Performance writes the D/W/M percents stored on a quote row.
// Stocks and futures use 5 and 21 sessions. Crypto uses 7 and 30 calendar days.
// DayPct on the quote wins for D; otherwise D is price versus PrevClose.
func Performance(class string, q Quote, bars []Bar, now time.Time) (d, w, m *float64) {
	if q.DayPct != nil {
		v := *q.DayPct
		d = &v
	} else if q.PrevClose > 0 && q.Price > 0 {
		v := (q.Price/q.PrevClose - 1) * 100
		d = &v
	}
	if q.Price <= 0 {
		return d, nil, nil
	}
	if class == Crypto {
		return d,
			changeVsDate(q.Price, bars, now.UTC().AddDate(0, 0, -7)),
			changeVsDate(q.Price, bars, now.UTC().AddDate(0, 0, -30))
	}
	return d, changeVsSessions(q.Price, bars, 5), changeVsSessions(q.Price, bars, 21)
}

// LookbackPct is the percent change from price to the last point at or before
// asOf minus lookback. A gap uses the previous print. No print that old returns nil.
func LookbackPct(price float64, points []Point, asOf time.Time, lookback time.Duration) *float64 {
	if price <= 0 || lookback <= 0 || asOf.IsZero() {
		return nil
	}
	target := asOf.Add(-lookback)
	var ref float64
	var refAt time.Time
	found := false
	for _, p := range points {
		if p.Price <= 0 || p.Time.After(target) {
			continue
		}
		if !found || !p.Time.Before(refAt) {
			ref = p.Price
			refAt = p.Time
			found = true
		}
	}
	if !found {
		return nil
	}
	v := (price/ref - 1) * 100
	return &v
}

func changeVsSessions(price float64, bars []Bar, sessions int) *float64 {
	if len(bars) <= sessions {
		return nil
	}
	ref := bars[len(bars)-1-sessions].Close
	if ref <= 0 {
		return nil
	}
	v := (price/ref - 1) * 100
	return &v
}

func changeVsDate(price float64, bars []Bar, target time.Time) *float64 {
	day := target.UTC().Format("2006-01-02")
	var ref float64
	for _, b := range bars {
		if b.Day <= day {
			ref = b.Close
			continue
		}
		break
	}
	if ref <= 0 {
		return nil
	}
	v := (price/ref - 1) * 100
	return &v
}

// Rollup folds daily bars into daily, weekly, or monthly candles.
// resolution is D, W, or M. Weekly buckets start on Monday. Monthly buckets
// start on the first of the month.
func Rollup(bars []Bar, resolution string) []Candle {
	if resolution == "" {
		resolution = "D"
	}
	out := make([]Candle, 0, len(bars))
	if resolution == "D" {
		for _, b := range bars {
			out = append(out, Candle{Time: b.Day, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
		}
		return out
	}
	index := map[string]int{}
	for _, b := range bars {
		key := bucket(b.Day, resolution)
		i, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, Candle{Time: key, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
			continue
		}
		c := &out[i]
		if b.High > c.High {
			c.High = b.High
		}
		if b.Low < c.Low {
			c.Low = b.Low
		}
		c.Close = b.Close
	}
	return out
}

func bucket(day, resolution string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	if resolution == "M" {
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, -(wd - 1)).Format("2006-01-02")
}

// NeedsTail reports whether a symbol's daily cache should be refreshed.
// A missing latest session is stale. Today's in-progress bar is stale after 15 minutes.
// A completed session (for example Friday, checked on Saturday) is left alone.
func NeedsTail(class, lastDay string, syncedAt *time.Time, now time.Time) bool {
	if lastDay == "" {
		return false
	}
	session := SessionDay(class, now)
	if lastDay < session {
		return true
	}
	today := todayFor(class, now)
	if lastDay == today && (syncedAt == nil || now.Sub(*syncedAt) > 15*time.Minute) {
		return true
	}
	return false
}

// SessionDay is the latest trading date we expect a bar for.
// Crypto uses the UTC date. Stocks and futures use the New York date, rolled back off the weekend.
func SessionDay(class string, now time.Time) string {
	if class == Crypto {
		return now.UTC().Format("2006-01-02")
	}
	t := now.In(newYork())
	for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

func todayFor(class string, now time.Time) string {
	if class == Crypto {
		return now.UTC().Format("2006-01-02")
	}
	return now.In(newYork()).Format("2006-01-02")
}

var (
	nyOnce sync.Once
	nyLoc  *time.Location
)

func newYork() *time.Location {
	nyOnce.Do(func() {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			loc = time.FixedZone("EST", -5*3600)
		}
		nyLoc = loc
	})
	return nyLoc
}

// NYDay converts a unix timestamp (seconds or milliseconds) to a New York session date.
func NYDay(ts int64) string {
	if ts > 1e12 {
		ts = ts / 1000
	}
	return time.Unix(ts, 0).In(newYork()).Format("2006-01-02")
}

// UTCDay converts a unix timestamp in milliseconds to a UTC date.
func UTCDay(ms int64) string {
	if ms < 1e12 {
		ms = ms * 1000
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

// FormatNumber renders a price or level without trailing zeros.
func FormatNumber(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	abs := math.Abs(v)
	digits := 2
	if abs > 0 && abs < 1 {
		if abs < 0.01 {
			digits = 6
		} else {
			digits = 4
		}
	}
	s := strconv.FormatFloat(v, 'f', digits, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// ScaleOHLC multiplies open, high, and low by adj/close and sets close to adj.
// Used so stock splits do not appear as crashes. A zero close is left untouched.
func ScaleOHLC(open, high, low, close, adj float64) (float64, float64, float64, float64) {
	if close == 0 || adj == 0 || math.IsNaN(adj) {
		return open, high, low, close
	}
	scale := adj / close
	return open * scale, high * scale, low * scale, adj
}
