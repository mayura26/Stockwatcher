package market

import "time"

const (
	Interval15m = "15m"
	Interval60m = "60m"
	Interval1d  = "1d"
)

const (
	// FetchNone means the stored series is already current.
	FetchNone = iota
	// FetchFull replaces a missing or short series.
	FetchFull
	// FetchTail asks only from the last stored open.
	FetchTail
)

// Span is one chart range. Interval is the series that is stored.
// Fold is the UTC block size, in seconds, when candles are grouped at read time.
type Span struct {
	Name     string
	Interval string
	Fold     int64
}

// LookupSpan resolves a chart range. The candle size is fixed for each range.
func LookupSpan(name string) (Span, bool) {
	switch name {
	case "D":
		return Span{Name: name, Interval: Interval15m}, true
	case "3D":
		return Span{Name: name, Interval: Interval60m}, true
	case "1W":
		return Span{Name: name, Interval: Interval60m, Fold: 2 * 3600}, true
	case "1M":
		return Span{Name: name, Interval: Interval60m, Fold: 4 * 3600}, true
	case "3M", "6M", "YTD", "1Y", "5Y":
		return Span{Name: name, Interval: Interval1d}, true
	default:
		return Span{}, false
	}
}

// Intraday reports whether the span is built from 15-minute or hourly bars.
func (s Span) Intraday() bool { return s.Interval == Interval15m || s.Interval == Interval60m }

// CacheKeep is how long an intraday series is kept.
// Five days of 15-minute bars still hold Friday on Monday.
// Sixty days of hourly bars covers the one-month span.
func CacheKeep(interval string) time.Duration {
	if interval == Interval15m {
		return 5 * 24 * time.Hour
	}
	return 60 * 24 * time.Hour
}

// CacheStart is the oldest open time retained for an interval.
func CacheStart(interval string, now time.Time) int64 {
	return now.Add(-CacheKeep(interval)).Unix()
}

// YahooRange is the chart range that fills a cold intraday cache.
func YahooRange(interval string) string {
	if interval == Interval15m {
		return "5d"
	}
	return "60d"
}

// IntervalStep is the bar width in seconds.
func IntervalStep(interval string) int64 {
	if interval == Interval15m {
		return 15 * 60
	}
	return 3600
}

// BucketOpen is the start of the interval that contains now.
func BucketOpen(now time.Time, interval string) int64 {
	step := IntervalStep(interval)
	u := now.Unix()
	if step <= 0 {
		return u
	}
	return u - (u % step)
}

// CacheCovers reports whether the oldest bar reaches the retention window.
// A weekend can push the first print a few days after the window start.
func CacheCovers(oldest, windowStart int64, interval string) bool {
	grace := int64(3 * 24 * 3600)
	if interval == Interval60m {
		grace = int64(4 * 24 * 3600)
	}
	return oldest <= windowStart+grace
}

// CacheFresh reports whether the newest bar is still the current bucket.
func CacheFresh(latest int64, now time.Time, interval string) bool {
	if latest <= 0 {
		return false
	}
	return latest >= BucketOpen(now, interval)
}

// IntradayFetch decides how a chart open should use the stored series.
// have must be ordered by open time.
func IntradayFetch(have []IntradayBar, windowStart int64, now time.Time, interval string) int {
	if len(have) == 0 || !CacheCovers(have[0].OpenUnix, windowStart, interval) {
		return FetchFull
	}
	if CacheFresh(have[len(have)-1].OpenUnix, now, interval) {
		return FetchNone
	}
	return FetchTail
}

// ShapeSpan turns stored intraday bars into the candles for a span.
func ShapeSpan(span Span, bars []IntradayBar) []Candle {
	if span.Fold > 0 {
		return FoldStep(bars, span.Fold)
	}
	return ToCandles(bars)
}

// DailyCandles slices stored sessions to the span and returns one candle per day.
func DailyCandles(class, name string, bars []Bar, now time.Time) []Candle {
	return Rollup(SliceDaily(class, name, bars, now), "D")
}

// SliceDaily returns the sessions shown for a daily span.
func SliceDaily(class, name string, bars []Bar, now time.Time) []Bar {
	switch name {
	case "YTD":
		return barsFromDay(bars, yearStart(class, now))
	case "5Y":
		return barsFromDay(bars, fiveYearDay(class, now))
	}
	if class == Crypto {
		start, ok := SpanStartUnix(class, name, now)
		if !ok {
			return append([]Bar(nil), bars...)
		}
		return barsFromDay(bars, time.Unix(start, 0).UTC().Format("2006-01-02"))
	}
	day, ok := SpanStartDay(class, name, bars, now)
	if !ok {
		return append([]Bar(nil), bars...)
	}
	return barsFromDay(bars, day)
}

// TrimIntraday keeps the bars that fall inside the span.
func TrimIntraday(class, name string, bars []IntradayBar, daily []Bar, now time.Time) []IntradayBar {
	if class == Crypto {
		start, ok := SpanStartUnix(class, name, now)
		if !ok {
			return append([]IntradayBar(nil), bars...)
		}
		out := make([]IntradayBar, 0, len(bars))
		for _, b := range bars {
			if b.OpenUnix >= start {
				out = append(out, b)
			}
		}
		return out
	}
	day, ok := SpanStartDay(class, name, daily, now)
	if !ok {
		return append([]IntradayBar(nil), bars...)
	}
	out := make([]IntradayBar, 0, len(bars))
	for _, b := range bars {
		if NYDay(b.OpenUnix) >= day {
			out = append(out, b)
		}
	}
	return out
}

// SpanStartDay is the first New York session included in a stock or futures span.
func SpanStartDay(class, name string, bars []Bar, now time.Time) (string, bool) {
	if class == Crypto {
		return "", false
	}
	switch name {
	case "YTD":
		return yearStart(class, now), true
	case "5Y":
		return fiveYearDay(class, now), true
	}
	n, ok := sessionCount(name)
	if !ok {
		return "", false
	}
	days := sessionDays(class, bars, now)
	if len(days) == 0 {
		return SessionDay(class, now), true
	}
	if len(days) <= n {
		return days[0], true
	}
	return days[len(days)-n], true
}

// SpanStartUnix is the first instant included in a crypto span.
func SpanStartUnix(class, name string, now time.Time) (int64, bool) {
	if class != Crypto {
		return 0, false
	}
	switch name {
	case "D":
		return now.UTC().Add(-24 * time.Hour).Unix(), true
	case "YTD":
		t := now.UTC()
		return time.Date(t.Year(), time.January, 1, 0, 0, 0, 0, time.UTC).Unix(), true
	case "5Y":
		return now.UTC().AddDate(-5, 0, 0).Unix(), true
	}
	days, ok := calendarDays(name)
	if !ok {
		return 0, false
	}
	return now.UTC().AddDate(0, 0, -days).Unix(), true
}

// RangeBase is the close a span's percent is measured from.
// D is the quote's previous close, so this reports no base for it.
// A span with no bar that old reports false.
func RangeBase(class, name string, bars []Bar, now time.Time) (float64, bool) {
	if name == "D" {
		return 0, false
	}
	switch name {
	case "YTD":
		return closeBefore(bars, yearStart(class, now))
	case "5Y":
		return closeOnOrBefore(bars, fiveYearDay(class, now))
	}
	if class == Crypto {
		start, ok := SpanStartUnix(class, name, now)
		if !ok {
			return 0, false
		}
		return closeOnOrBefore(bars, time.Unix(start, 0).UTC().Format("2006-01-02"))
	}
	day, ok := SpanStartDay(class, name, bars, now)
	if !ok {
		return 0, false
	}
	return closeBefore(bars, day)
}

// RangeMove is the percent from RangeBase to price, and that base.
func RangeMove(class, name string, price float64, bars []Bar, now time.Time) (pct *float64, base float64) {
	if price <= 0 {
		return nil, 0
	}
	ref, ok := RangeBase(class, name, bars, now)
	if !ok || ref <= 0 {
		return nil, 0
	}
	v := (price/ref - 1) * 100
	return &v, ref
}

func sessionCount(name string) (int, bool) {
	switch name {
	case "D":
		return 1, true
	case "3D":
		return 3, true
	case "1W":
		return 5, true
	case "1M":
		return 21, true
	case "3M":
		return 63, true
	case "6M":
		return 126, true
	case "1Y":
		return 252, true
	default:
		return 0, false
	}
}

func calendarDays(name string) (int, bool) {
	switch name {
	case "D":
		return 1, true
	case "3D":
		return 3, true
	case "1W":
		return 7, true
	case "1M":
		return 30, true
	case "3M":
		return 90, true
	case "6M":
		return 180, true
	case "1Y":
		return 365, true
	default:
		return 0, false
	}
}

func sessionDays(class string, bars []Bar, now time.Time) []string {
	var days []string
	for _, b := range bars {
		if b.Day == "" {
			continue
		}
		if len(days) == 0 || days[len(days)-1] != b.Day {
			days = append(days, b.Day)
		}
	}
	session := SessionDay(class, now)
	if session != "" && (len(days) == 0 || days[len(days)-1] < session) {
		days = append(days, session)
	}
	return days
}

func yearStart(class string, now time.Time) string {
	t := now.UTC()
	if class != Crypto {
		t = now.In(newYork())
	}
	return time.Date(t.Year(), time.January, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func fiveYearDay(class string, now time.Time) string {
	t := now.UTC()
	if class != Crypto {
		t = now.In(newYork())
	}
	return t.AddDate(-5, 0, 0).Format("2006-01-02")
}

func barsFromDay(bars []Bar, day string) []Bar {
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if b.Day >= day {
			out = append(out, b)
		}
	}
	return out
}

func closeBefore(bars []Bar, day string) (float64, bool) {
	var ref float64
	ok := false
	for _, b := range bars {
		if b.Day >= day {
			break
		}
		if b.Close > 0 {
			ref = b.Close
			ok = true
		}
	}
	return ref, ok
}

func closeOnOrBefore(bars []Bar, day string) (float64, bool) {
	var ref float64
	ok := false
	for _, b := range bars {
		if b.Day > day {
			break
		}
		if b.Close > 0 {
			ref = b.Close
			ok = true
		}
	}
	return ref, ok
}
