package market

import (
	"encoding/json"
	"math"
	"sort"
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
// ExtSet means this refresh decided the extended-hours percent. A failed load
// leaves ExtSet false so a stored Pre or After stays put. ExtSession is
// "pre" or "post" when ExtPct should be shown.
type Quote struct {
	Price       float64
	PrevClose   float64
	DayPct      *float64
	AsOf        time.Time
	Intraday    []Point
	IntradaySet bool
	ExtSession  string
	ExtPct      *float64
	ExtSet      bool
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

// Candle is one Lightweight Charts bar. Time is a business day for D, W, and M,
// and a unix-second open for 5m, 1H, and 4H.
type Candle struct {
	Time  CandleTime `json:"time"`
	Open  float64    `json:"open"`
	High  float64    `json:"high"`
	Low   float64    `json:"low"`
	Close float64    `json:"close"`
}

// CandleTime marshals as a date string or a unix second, which is what the chart accepts.
type CandleTime struct {
	Day  string
	Unix int64
}

// DayTime is a business-day chart time.
func DayTime(day string) CandleTime { return CandleTime{Day: day} }

// UnixTime is an intraday bar open, in seconds.
func UnixTime(sec int64) CandleTime { return CandleTime{Unix: sec} }

func (t CandleTime) MarshalJSON() ([]byte, error) {
	if t.Day != "" {
		return json.Marshal(t.Day)
	}
	return json.Marshal(t.Unix)
}

// IntradayBar is one intraday candle. OpenUnix is the bar open in seconds.
type IntradayBar struct {
	OpenUnix        int64
	Open, High, Low float64
	Close           float64
	Volume          float64
}

// Hit is a search result the client can add to a group.
type Hit struct {
	AssetClass     string `json:"asset_class"`
	Display        string `json:"display"`
	ProviderSymbol string `json:"provider_symbol"`
	Name           string `json:"name"`
}

// Performance writes the D/W/M/YTD/1Y percents stored on a quote row.
// Stocks and futures use 5 and 21 sessions for W and M. Crypto uses 7 and 30 calendar days.
// YTD and the rolling year use the same bases as the chart spans.
// DayPct on the quote wins for D; otherwise D is price versus PrevClose.
func Performance(class string, q Quote, bars []Bar, now time.Time) (d, w, m, ytd, y1 *float64) {
	if q.DayPct != nil {
		v := *q.DayPct
		d = &v
	} else if q.PrevClose > 0 && q.Price > 0 {
		v := (q.Price/q.PrevClose - 1) * 100
		d = &v
	}
	if q.Price <= 0 {
		return d, nil, nil, nil, nil
	}
	ytd, _ = RangeMove(class, "YTD", q.Price, bars, now)
	y1, _ = RangeMove(class, "1Y", q.Price, bars, now)
	if class == Crypto {
		return d,
			changeVsDate(q.Price, bars, now.UTC().AddDate(0, 0, -7)),
			changeVsDate(q.Price, bars, now.UTC().AddDate(0, 0, -30)),
			ytd, y1
	}
	return d, changeVsSessions(q.Price, bars, 5), changeVsSessions(q.Price, bars, 21), ytd, y1
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
			out = append(out, Candle{Time: DayTime(b.Day), Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
		}
		return out
	}
	index := map[string]int{}
	for _, b := range bars {
		key := bucket(b.Day, resolution)
		i, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, Candle{Time: DayTime(key), Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
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

// ToCandles copies intraday bars through with their open time.
func ToCandles(bars []IntradayBar) []Candle {
	out := make([]Candle, 0, len(bars))
	for _, b := range bars {
		if b.Close == 0 {
			continue
		}
		out = append(out, Candle{Time: UnixTime(b.OpenUnix), Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
	}
	return out
}

// Fold2H groups hourly bars into UTC blocks on even hours.
func Fold2H(bars []IntradayBar) []Candle { return FoldStep(bars, 2*3600) }

// Fold4H groups hourly bars into UTC blocks at 00:00, 04:00, 08:00, 12:00, 16:00, and 20:00.
// A bar that starts inside a block joins that block.
func Fold4H(bars []IntradayBar) []Candle { return FoldStep(bars, 4*3600) }

// FoldStep groups intraday bars into UTC blocks of step seconds.
// A bar that starts inside a block joins that block.
func FoldStep(bars []IntradayBar, step int64) []Candle {
	if step <= 0 {
		return ToCandles(bars)
	}
	ordered := append([]IntradayBar(nil), bars...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].OpenUnix < ordered[j].OpenUnix })
	out := make([]Candle, 0, len(ordered)/4+1)
	index := map[int64]int{}
	for _, b := range ordered {
		if b.Close == 0 || b.OpenUnix < 0 {
			continue
		}
		key := b.OpenUnix - (b.OpenUnix % step)
		i, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, Candle{Time: UnixTime(key), Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
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

// SessionCloses is the sparkline for the last market session.
// Stocks and futures use the New York session, rolled back to Friday on a weekend.
// Crypto uses the 24 hours ending at asOf.
func SessionCloses(class string, points []Point, asOf time.Time) []float64 {
	if len(points) == 0 {
		return nil
	}
	ordered := append([]Point(nil), points...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Time.Before(ordered[j].Time) })
	if asOf.IsZero() {
		asOf = ordered[len(ordered)-1].Time
	}
	if class == Crypto {
		cut := asOf.Add(-24 * time.Hour)
		out := make([]float64, 0, len(ordered))
		for _, p := range ordered {
			if p.Price <= 0 || p.Time.Before(cut) {
				continue
			}
			out = append(out, p.Price)
		}
		return out
	}
	day := SessionDay(class, asOf)
	out := make([]float64, 0, len(ordered))
	for _, p := range ordered {
		if p.Price <= 0 {
			continue
		}
		if NYDay(p.Time.Unix()) == day {
			out = append(out, p.Price)
		}
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

// sessionForming reports whether the bar for today's session is still incomplete.
// Stocks stay open until 16:00 ET, futures until 17:00 ET. Crypto's UTC date
// rolls at midnight, so the current UTC day is always incomplete. A weekend
// stock or futures session is already complete.
func sessionForming(class string, now time.Time) bool {
	if class == Crypto {
		return true
	}
	t := now.In(newYork())
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	mins := t.Hour()*60 + t.Minute()
	if class == Future {
		return mins < 17*60
	}
	return mins < 16*60
}

// CompletedBars drops the in-progress session. bars must be ordered by day.
// A closed session, including a weekend looking back at Friday, is kept whole.
func CompletedBars(class string, bars []Bar, now time.Time) []Bar {
	if len(bars) == 0 || !sessionForming(class, now) {
		return bars
	}
	today := todayFor(class, now)
	i := len(bars)
	for i > 0 && bars[i-1].Day >= today {
		i--
	}
	return bars[:i]
}

// CompletedDay is the latest finished session given only the two newest bar
// dates. prevDay is the session before lastDay. An empty string means there
// is no completed session yet.
func CompletedDay(class, lastDay, prevDay string, now time.Time) string {
	if lastDay == "" {
		return ""
	}
	if sessionForming(class, now) && lastDay >= todayFor(class, now) {
		return prevDay
	}
	return lastDay
}

// ScoredDay is the session whose move the blotter is still showing. A closed
// session stays the scored day through the weekend, so the band keeps using
// the prior close. Once the next session has started, that new day is scored
// even if its bar is not stored yet.
func ScoredDay(class, lastDay string, now time.Time) string {
	if lastDay == "" {
		return ""
	}
	today := todayFor(class, now)
	if sessionForming(class, now) && lastDay < today {
		return today
	}
	return lastDay
}

// BandBasis is the session the expected band is centered on: the close before
// the scored day. prevDay is the bar before lastDay. An empty string means
// there is no prior close yet.
func BandBasis(class, lastDay, prevDay string, now time.Time) string {
	scored := ScoredDay(class, lastDay, now)
	if scored == "" {
		return ""
	}
	if lastDay < scored {
		return lastDay
	}
	return prevDay
}

// BandBars drops the session being scored. The expected move is then centered
// on the prior close, and the day's own return is not part of the deviation.
// bars must be ordered by day.
func BandBars(class string, bars []Bar, now time.Time) []Bar {
	if len(bars) == 0 {
		return bars
	}
	scored := ScoredDay(class, bars[len(bars)-1].Day, now)
	if scored == "" {
		return bars
	}
	i := len(bars)
	for i > 0 && bars[i-1].Day >= scored {
		i--
	}
	return bars[:i]
}

// ExpectedBand is the last close ± one sample standard deviation of the last
// 20 close-to-close simple returns. The band is centered on that close, with
// no drift term. ok is false with fewer than 21 closes or a zero deviation,
// which would make every print look unusual.
func ExpectedBand(bars []Bar) (low, high float64, ok bool) {
	if len(bars) < 21 {
		return 0, 0, false
	}
	window := bars[len(bars)-21:]
	rets := make([]float64, 20)
	for i := 1; i < len(window); i++ {
		prev := window[i-1].Close
		cur := window[i].Close
		if prev <= 0 || cur <= 0 {
			return 0, 0, false
		}
		rets[i-1] = cur/prev - 1
	}
	var mean float64
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	var ss float64
	for _, r := range rets {
		d := r - mean
		ss += d * d
	}
	sigma := math.Sqrt(ss / float64(len(rets)-1))
	if sigma == 0 || math.IsNaN(sigma) || math.IsInf(sigma, 0) {
		return 0, 0, false
	}
	last := window[len(window)-1].Close
	return last * (1 - sigma), last * (1 + sigma), true
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
