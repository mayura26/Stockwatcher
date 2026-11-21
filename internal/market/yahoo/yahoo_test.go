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

func TestParseIntraday(t *testing.T) {
	raw := []byte(`{"chart":{"result":[{"timestamp":[1700000000,1700000300,1700000600],"indicators":{"quote":[{"close":[10,null,11.5]}]}}],"error":null}}`)
	pts, ext, err := ParseIntraday(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 || pts[0].Price != 10 || pts[1].Price != 11.5 {
		t.Fatalf("%+v", pts)
	}
	if !pts[0].Time.Equal(time.Unix(1700000000, 0).Add(5*time.Minute)) || !pts[1].Time.Equal(time.Unix(1700000600, 0).Add(5*time.Minute)) {
		t.Fatalf("times %+v", pts)
	}
	if ext.Session != "" || ext.Pct != nil {
		t.Fatalf("extended %+v", ext)
	}
	if _, _, err := ParseIntraday([]byte(`{"chart":{"result":null,"error":{"description":"No data found"}}}`)); err == nil || err.Error() != "No data found" {
		t.Fatal(err)
	}
}

func TestParseExtended(t *testing.T) {
	const periods = `{"pre":[[{"start":1000,"end":2000}],[{"start":5000,"end":6000}]],"regular":[[{"start":100,"end":500}],[{"start":2000,"end":3000}]],"post":[[{"start":3000,"end":4000}]]}`
	chart := func(ts, closes, price string) []byte {
		return []byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":` + price + `,"tradingPeriods":` + periods + `},"timestamp":` + ts + `,"indicators":{"quote":[{"close":` + closes + `}]}}],"error":null}}`)
	}
	cases := []struct {
		name    string
		ts      string
		closes  string
		price   string
		session string
		pct     *float64
		points  int
	}{
		{"pre", "[400,1500]", "[100,103]", "100", "pre", fptr(3), 1},
		{"post", "[1500,2500,3500]", "[101,110,98]", "100", "post", fptr(-2), 1},
		{"closed later after", "[1500,2500,3500]", "[101,110,102]", "100", "post", fptr(2), 1},
		{"closed later pre", "[2500,3500,5500]", "[110,102,101.5]", "100", "pre", fptr(1.5), 1},
		{"regular", "[1500,2500]", "[101,110]", "100", "", nil, 1},
		{"zero price", "[2500,3500]", "[110,0]", "100", "", nil, 1},
		{"flat", "[2500,3500]", "[110,100]", "100", "post", fptr(0), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pts, ext, err := ParseIntraday(chart(tc.ts, tc.closes, tc.price))
			if err != nil {
				t.Fatal(err)
			}
			if len(pts) != tc.points {
				t.Fatalf("points %+v", pts)
			}
			if ext.Session != tc.session {
				t.Fatalf("session %q", ext.Session)
			}
			if tc.pct == nil {
				if ext.Pct != nil {
					t.Fatalf("pct %v", *ext.Pct)
				}
				return
			}
			if ext.Pct == nil || math.Abs(*ext.Pct-*tc.pct) > 1e-9 {
				t.Fatalf("pct %v", ext.Pct)
			}
		})
	}
}

func fptr(v float64) *float64 { return &v }

func TestParseCandles(t *testing.T) {
	raw := []byte(`{"chart":{"result":[{"timestamp":[1700000000,1700000300],"indicators":{"quote":[{"open":[9,null],"high":[11,12],"low":[8,9],"close":[10,11]}]}}],"error":null}}`)
	bars, err := ParseCandles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 || bars[0].OpenUnix != 1700000000 || bars[0].Open != 9 || bars[0].High != 11 || bars[0].Low != 8 || bars[0].Close != 10 {
		t.Fatalf("first %+v", bars)
	}
	if bars[1].OpenUnix != 1700000300 || bars[1].Open != 11 || bars[1].Close != 11 {
		t.Fatalf("second %+v", bars[1])
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
