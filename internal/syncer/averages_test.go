package syncer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/binance"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
)

func TestRefreshAveragesSkipsUnchangedClose(t *testing.T) {
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
		bars[i] = market.Bar{Day: day.Format("2006-01-02"), Open: 1, High: 2, Low: 1, Close: float64(i + 1)}
	}
	if err := st.UpsertBars(ctx, sym.ID, bars); err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{}, &yahoo.Client{}, &binance.Client{}, nil)
	n, err := sy.RefreshAverages(ctx)
	if err != nil || n != 3 {
		t.Fatalf("first %d %v", n, err)
	}
	points, err := st.AveragePoints(ctx, sym.ID, 1)
	if err != nil || len(points) == 0 || points[len(points)-1].Day != "2024-06-14" {
		t.Fatalf("points %+v %v", points, err)
	}
	n, err = sy.RefreshAverages(ctx)
	if err != nil || n != 0 {
		t.Fatalf("second %d %v", n, err)
	}
	bars[len(bars)-1].Close = 99
	if err := st.UpsertBars(ctx, sym.ID, bars); err != nil {
		t.Fatal(err)
	}
	n, err = sy.RefreshSymbolAverages(ctx, sym.ID)
	if err != nil || n != 3 {
		t.Fatalf("tail %d %v", n, err)
	}
	points, err = st.AveragePoints(ctx, sym.ID, 1)
	if err != nil || len(points) == 0 || points[len(points)-1].Value == 0 {
		t.Fatalf("rebuilt %+v %v", points, err)
	}
}
