package yahoo

import (
	"math"
	"os"
	"testing"
	"time"

	"stockwatcher/internal/market"
)

func TestParseESQuote(t *testing.T) {
	raw, err := os.ReadFile("testdata/es.json")
	if err != nil {
		t.Fatal(err)
	}
	q, bars, err := ParseChart(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 6966.75 {
		t.Fatalf("price %v", q.Price)
	}
	if q.DayPct == nil || math.Abs(*q.DayPct-(-3.203)) > 1e-9 {
		t.Fatalf("day pct %v", q.DayPct)
	}
	if len(bars) != 5 {
		t.Fatalf("bars %d", len(bars))
	}
	if bars[len(bars)-2].Close != 7197.25 {
		t.Fatalf("prev session %v", bars[len(bars)-2].Close)
	}
	if q.PrevClose != 7197.25 {
		t.Fatalf("prev close %v", q.PrevClose)
	}
	wantDay := time.Unix(1794805200, 0).In(mustNY(t)).Format("2006-01-02")
	if bars[0].Day != wantDay {
		t.Fatalf("day %s want %s", bars[0].Day, wantDay)
	}
}

func TestParseAdjustsStockOHLC(t *testing.T) {
	raw := []byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":50,"previousClose":40,"regularMarketTime":1700000000,"symbol":"AAA"},"timestamp":[1700000000],"indicators":{"quote":[{"open":[100],"high":[110],"low":[90],"close":[100],"volume":[10]}],"adjclose":[{"adjclose":[50]}]}}],"error":null}}`)
	q, bars, err := ParseChart(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if q.PrevClose != 40 || q.Price != 50 {
		t.Fatalf("quote %+v", q)
	}
	if len(bars) != 1 || bars[0].Open != 50 || bars[0].High != 55 || bars[0].Low != 45 || bars[0].Close != 50 {
		t.Fatalf("bar %+v", bars)
	}
}

func TestParseYahooError(t *testing.T) {
	_, _, err := ParseChart([]byte(`{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found"}}}`), false)
	if err == nil || err.Error() != "No data found" {
		t.Fatal(err)
	}
}

func mustNY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	_ = market.NYDay(0)
	return loc
}
