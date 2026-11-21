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
