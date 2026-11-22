package market

import (
	"testing"
	"time"
)

func TestSpanWindowAndChange(t *testing.T) {
	now := time.Date(2024, 1, 10, 15, 0, 0, 0, time.UTC)
	bars := []Bar{
		{Day: "2023-12-29", Close: 50},
		{Day: "2024-01-05", Close: 80},
		{Day: "2024-01-08", Close: 90},
		{Day: "2024-01-09", Close: 100},
		{Day: "2024-01-10", Close: 110},
	}
	start, ok := SpanStartDay(Stock, "3D", bars, now)
	if !ok || start != "2024-01-08" {
		t.Fatalf("3D start %s %v", start, ok)
	}
	got := SliceDaily(Stock, "3D", bars, now)
	if len(got) != 3 || got[0].Day != "2024-01-08" || got[2].Day != "2024-01-10" {
		t.Fatalf("3D bars %+v", got)
	}
	pct, base := RangeMove(Stock, "3D", 110, bars, now)
	if pct == nil || base != 80 {
		t.Fatalf("3D move %v %v", pct, base)
	}
	near(t, *pct, (110.0/80.0-1)*100)

	ytd := SliceDaily(Stock, "YTD", bars, now)
	if len(ytd) != 4 || ytd[0].Day != "2024-01-05" {
		t.Fatalf("ytd %+v", ytd)
	}
	pct, base = RangeMove(Stock, "YTD", 110, bars, now)
	if pct == nil || base != 50 {
		t.Fatalf("ytd move %v %v", pct, base)
	}
	near(t, *pct, (110.0/50.0-1)*100)

	if _, ok := RangeBase(Stock, "5Y", bars, now); ok {
		t.Fatal("short 5Y history should have no base")
	}
	five := SliceDaily(Stock, "5Y", bars, now)
	if len(five) != len(bars) {
		t.Fatalf("short 5Y still shows the bars we have, got %d", len(five))
	}

	cryptoNow := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	crypto := []Bar{
		{Day: "2023-12-16", Close: 40},
		{Day: "2024-01-08", Close: 70},
		{Day: "2024-01-15", Close: 100},
	}
	pct, base = RangeMove(Crypto, "1W", 100, crypto, cryptoNow)
	if pct == nil || base != 70 {
		t.Fatalf("crypto week %v %v", pct, base)
	}
	near(t, *pct, (100.0/70.0-1)*100)
	week := SliceDaily(Crypto, "1W", crypto, cryptoNow)
	if len(week) != 2 || week[0].Day != "2024-01-08" {
		t.Fatalf("crypto week bars %+v", week)
	}
}

func TestFold2H(t *testing.T) {
	bars := []IntradayBar{
		{OpenUnix: 0, Open: 1, High: 2, Low: 1, Close: 1.5},
		{OpenUnix: 3600, Open: 1.5, High: 5, Low: 0.5, Close: 3},
		{OpenUnix: 2 * 3600, Open: 3, High: 4, Low: 2.5, Close: 3.5},
	}
	got := Fold2H(bars)
	if len(got) != 2 {
		t.Fatalf("%d buckets", len(got))
	}
	if got[0].Time.Unix != 0 || got[0].Open != 1 || got[0].High != 5 || got[0].Low != 0.5 || got[0].Close != 3 {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].Time.Unix != 2*3600 || got[1].Close != 3.5 {
		t.Fatalf("second %+v", got[1])
	}
}

func TestIntradayFetchPlan(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	start := CacheStart(Interval15m, now)
	fresh := BucketOpen(now, Interval15m)
	if IntradayFetch(nil, start, now, Interval15m) != FetchFull {
		t.Fatal("empty cache should fetch the window")
	}
	covered := []IntradayBar{{OpenUnix: start}, {OpenUnix: fresh}}
	if IntradayFetch(covered, start, now, Interval15m) != FetchNone {
		t.Fatal("current bucket should skip the provider")
	}
	stale := []IntradayBar{{OpenUnix: start}, {OpenUnix: fresh - IntervalStep(Interval15m)}}
	if IntradayFetch(stale, start, now, Interval15m) != FetchTail {
		t.Fatal("stale cache should fetch the tail")
	}
	short := []IntradayBar{{OpenUnix: fresh}}
	if IntradayFetch(short, start, now, Interval15m) != FetchFull {
		t.Fatal("a cache that misses the window start should be refilled")
	}
}

func TestFoldSessions(t *testing.T) {
	bars := []Bar{
		{Day: "2024-01-02", Open: 1, High: 2, Low: 1, Close: 1.5},
		{Day: "2024-01-03", Open: 1.5, High: 5, Low: 0.5, Close: 3},
		{Day: "2024-01-04", Open: 3, High: 4, Low: 2.5, Close: 3.5},
		{Day: "2024-01-05", Open: 3.5, High: 6, Low: 3, Close: 4},
		{Day: "2024-01-08", Open: 4, High: 4.5, Low: 3.5, Close: 4.2},
		{Day: "2024-01-09", Open: 4.2, High: 7, Low: 4, Close: 6},
		{Day: "2024-01-10", Open: 6, High: 6.5, Low: 5.5, Close: 6.2},
	}
	got := FoldDaily(bars, "3S")
	if len(got) != 3 {
		t.Fatalf("%d candles", len(got))
	}
	if got[0].Time.Day != "2024-01-04" || got[0].Open != 1 || got[0].High != 5 || got[0].Low != 0.5 || got[0].Close != 3.5 {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].Time.Day != "2024-01-09" || got[1].Open != 3.5 || got[1].High != 7 || got[1].Low != 3 || got[1].Close != 6 {
		t.Fatalf("second %+v", got[1])
	}
	if got[2].Time.Day != "2024-01-10" || got[2].Open != 6 || got[2].Close != 6.2 {
		t.Fatalf("tail %+v", got[2])
	}
}

func TestFoldWeekFeedsAverage(t *testing.T) {
	bars := []Bar{
		{Day: "2024-01-01", Open: 1, High: 3, Low: 0.5, Close: 2},
		{Day: "2024-01-02", Open: 2, High: 4, Low: 1.5, Close: 3},
		{Day: "2024-01-05", Open: 3, High: 3.5, Low: 2.5, Close: 3.2},
		{Day: "2024-01-08", Open: 4, High: 6, Low: 3.5, Close: 5},
	}
	week := FoldDaily(bars, "W")
	if len(week) != 2 || week[0].Time.Day != "2024-01-05" || week[0].Open != 1 || week[0].High != 4 || week[0].Low != 0.5 || week[0].Close != 3.2 {
		t.Fatalf("first week %+v", week)
	}
	if week[1].Time.Day != "2024-01-08" || week[1].Close != 5 {
		t.Fatalf("second week %+v", week[1])
	}
	points := []AveragePoint{
		{Day: "2024-01-02", Value: 1},
		{Day: "2024-01-05", Value: 2.5},
		{Day: "2024-01-08", Value: 4},
	}
	marks := AlignAverage(Stock, week, points)
	if len(marks) != 2 || marks[0].Time.Day != "2024-01-05" || marks[0].Value != 2.5 || marks[1].Value != 4 {
		t.Fatalf("average %+v", marks)
	}
}

func TestPickFold(t *testing.T) {
	if PickFold("6M", dailyBars("2024-01-01", 180), 0) != "D" {
		t.Fatal("missing width stays daily")
	}
	if PickFold("3M", dailyBars("2024-01-01", 180), 300) != "D" {
		t.Fatal("3M stays daily")
	}
	if PickFold("YTD", dailyBars("2024-01-01", 10), 300) != "D" {
		t.Fatal("short YTD stays daily")
	}
	if PickFold("6M", dailyBars("2024-01-01", 180), 300) != "3S" {
		t.Fatal("narrow 6M should step to 3 sessions")
	}
	if PickFold("1Y", dailyBars("2024-01-01", 365), 1100) != "3S" {
		t.Fatal("desktop 1Y should step to 3 sessions")
	}
	wide := dailyBars("2020-01-06", 1825)
	if PickFold("5Y", wide, 1100) != "M" {
		t.Fatal("desktop 5Y should use months")
	}
	if PickFold("5Y", wide, 1500) != "W" {
		t.Fatal("wide 5Y should use weeks")
	}
}

func dailyBars(start string, n int) []Bar {
	day, err := time.Parse("2006-01-02", start)
	if err != nil {
		panic(err)
	}
	out := make([]Bar, n)
	for i := 0; i < n; i++ {
		out[i] = Bar{Day: day.AddDate(0, 0, i).Format("2006-01-02"), Open: 1, High: 2, Low: 1, Close: float64(i + 1)}
	}
	return out
}
