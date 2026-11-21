package syncer

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/binance"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
)

func TestRefreshRangesSkipsCurrentBasis(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gid int64
	for _, g := range board.Groups {
		if g.Name == "Watch" {
			gid = g.ID
		}
	}
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	end := time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC)
	bars := make([]market.Bar, 30)
	for i := range bars {
		day := end.AddDate(0, 0, -(len(bars) - 1 - i))
		close := 100 + float64(i%5)
		bars[i] = market.Bar{Day: day.Format("2006-01-02"), Open: close, High: close + 1, Low: close - 1, Close: close}
	}
	if err := st.UpsertBars(ctx, sym.ID, bars); err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, ny)
	if err := st.UpsertRange(ctx, sym.ID, "2024-06-13", 1, 2, 1); err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{}, &yahoo.Client{}, &binance.Client{}, nil)
	sy.Now = func() time.Time { return now }
	if _, err := sy.RefreshRanges(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.Range(ctx, sym.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BasisDay != "2024-06-13" || got.Low != 1 || got.High != 2 || got.ComputedAt != 1 {
		t.Fatalf("current basis was rewritten: %+v", got)
	}

	if err := st.UpsertRange(ctx, sym.ID, "2024-06-12", 1, 2, 1); err != nil {
		t.Fatal(err)
	}
	if n, err := sy.RefreshRanges(ctx); err != nil || n != 1 {
		t.Fatal(err, n)
	}
	got, err = st.Range(ctx, sym.ID)
	if err != nil {
		t.Fatal(err)
	}
	low, high, ok := market.ExpectedBand(market.BandBars(market.Stock, bars, now))
	if !ok {
		t.Fatal("band")
	}
	if got.BasisDay != "2024-06-13" || got.ComputedAt != now.Unix() || math.Abs(got.Low-low) > 1e-9 || math.Abs(got.High-high) > 1e-9 {
		t.Fatalf("stale basis %+v want %v %v", got, low, high)
	}
}
