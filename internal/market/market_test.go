package market

import (
	"math"
	"testing"
	"time"
)

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPerformanceSessionsAndCalendar(t *testing.T) {
	bars := []Bar{
		{Day: "2024-01-01", Close: 80},
		{Day: "2024-01-02", Close: 90},
		{Day: "2024-01-03", Close: 100},
		{Day: "2024-01-04", Close: 100},
		{Day: "2024-01-05", Close: 100},
		{Day: "2024-01-08", Close: 110},
	}
	day := 1.5
	d, w, m := Performance(Stock, Quote{Price: 110, PrevClose: 100, DayPct: &day}, bars, time.Date(2024, 1, 8, 15, 0, 0, 0, time.UTC))
	if d == nil || *d != 1.5 {
		t.Fatalf("day = %v", d)
	}
	if w == nil {
		t.Fatal("week nil")
	}
	// 5 sessions back from the last bar is the first close, 80.
	near(t, *w, (110.0/80.0-1)*100)
	if m != nil {
		t.Fatal("month should be nil with only 6 bars")
	}

	crypto := []Bar{
		{Day: "2023-12-16", Close: 40},
		{Day: "2024-01-01", Close: 50},
		{Day: "2024-01-08", Close: 70},
		{Day: "2024-01-15", Close: 100},
	}
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	_, cw, cm := Performance(Crypto, Quote{Price: 100, PrevClose: 90}, crypto, now)
	if cw == nil {
		t.Fatal("crypto week nil")
	}
	near(t, *cw, (100.0/70.0-1)*100)
	if cm == nil {
		t.Fatal("crypto month nil")
	}
	near(t, *cm, (100.0/40.0-1)*100)
}

func TestLookbackPct(t *testing.T) {
	asOf := time.Date(2024, 1, 8, 15, 0, 0, 0, time.UTC)
	points := []Point{
		{Time: asOf.Add(-4 * time.Hour), Price: 80},
		{Time: asOf.Add(-1 * time.Hour), Price: 100},
		{Time: asOf, Price: 110},
	}
	h1 := LookbackPct(110, points, asOf, time.Hour)
	if h1 == nil {
		t.Fatal("1h nil")
	}
	near(t, *h1, 10)
	h4 := LookbackPct(110, points, asOf, 4*time.Hour)
	if h4 == nil {
		t.Fatal("4h nil")
	}
	near(t, *h4, (110.0/80.0-1)*100)

	gap := []Point{
		{Time: asOf.Add(-2 * time.Hour), Price: 50},
		{Time: asOf.Add(-30 * time.Minute), Price: 60},
	}
	got := LookbackPct(75, gap, asOf, time.Hour)
	if got == nil {
		t.Fatal("gap nil")
	}
	near(t, *got, (75.0/50.0-1)*100)

	short := []Point{{Time: asOf.Add(-30 * time.Minute), Price: 10}}
	if LookbackPct(12, short, asOf, time.Hour) != nil {
		t.Fatal("short history should be nil")
	}
	if LookbackPct(12, nil, asOf, time.Hour) != nil {
		t.Fatal("empty history should be nil")
	}
}

func TestRollupWeekAndMonth(t *testing.T) {
	bars := []Bar{
		{Day: "2024-01-01", Open: 1, High: 3, Low: 0.5, Close: 2},
		{Day: "2024-01-02", Open: 2, High: 4, Low: 1.5, Close: 3},
		{Day: "2024-01-05", Open: 3, High: 3.5, Low: 2.5, Close: 3.2},
		{Day: "2024-01-08", Open: 4, High: 6, Low: 3.5, Close: 5},
		{Day: "2024-02-01", Open: 5, High: 5, Low: 4, Close: 4.5},
	}
	week := Rollup(bars, "W")
	if len(week) != 3 {
		t.Fatalf("weeks = %d", len(week))
	}
	if week[0].Time != "2024-01-01" || week[0].Open != 1 || week[0].High != 4 || week[0].Low != 0.5 || week[0].Close != 3.2 {
		t.Fatalf("first week %+v", week[0])
	}
	if week[1].Time != "2024-01-08" || week[1].Close != 5 {
		t.Fatalf("second week %+v", week[1])
	}
	month := Rollup(bars, "M")
	if len(month) != 2 || month[0].Time != "2024-01-01" || month[0].Close != 5 || month[1].Time != "2024-02-01" {
		t.Fatalf("months %+v", month)
	}
	daily := Rollup(bars, "D")
	if len(daily) != len(bars) || daily[0].Time != "2024-01-01" {
		t.Fatalf("daily %+v", daily)
	}
}

func TestNeedsTail(t *testing.T) {
	wed := time.Date(2024, 1, 10, 16, 0, 0, 0, time.UTC) // Wednesday
	recent := wed.Add(-5 * time.Minute)
	old := wed.Add(-20 * time.Minute)
	if NeedsTail(Stock, "", nil, wed) {
		t.Fatal("empty last day is a full backfill, not a tail")
	}
	if !NeedsTail(Stock, "2024-01-09", &recent, wed) {
		t.Fatal("missing session should be stale")
	}
	if NeedsTail(Stock, "2024-01-10", &recent, wed) {
		t.Fatal("fresh today should not be stale")
	}
	if !NeedsTail(Stock, "2024-01-10", &old, wed) {
		t.Fatal("today older than 15m should be stale")
	}
	sat := time.Date(2024, 1, 13, 15, 0, 0, 0, time.UTC)
	fridaySync := sat.Add(-2 * time.Hour)
	if NeedsTail(Stock, "2024-01-12", &fridaySync, sat) {
		t.Fatal("friday bar on saturday should stay")
	}
}

func TestScaleOHLC(t *testing.T) {
	o, h, l, c := ScaleOHLC(100, 110, 90, 100, 50)
	near(t, o, 50)
	near(t, h, 55)
	near(t, l, 45)
	near(t, c, 50)
}

func TestFormatNumber(t *testing.T) {
	if FormatNumber(5800) != "5800" {
		t.Fatal(FormatNumber(5800))
	}
	if FormatNumber(0.1234) != "0.1234" {
		t.Fatal(FormatNumber(0.1234))
	}
}

func TestSearchFutures(t *testing.T) {
	all := SearchFutures("")
	if len(all) < 40 {
		t.Fatalf("curated list %d", len(all))
	}
	got := SearchFutures("es")
	if len(got) == 0 || got[0].ProviderSymbol != "ES=F" {
		t.Fatalf("%+v", got)
	}
	custom := SearchFutures("SIL=F")
	found := false
	for _, h := range custom {
		if h.ProviderSymbol == "SIL=F" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing manual symbol %+v", custom)
	}
}
