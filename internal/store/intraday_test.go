package store

import (
	"context"
	"path/filepath"
	"testing"

	"stockwatcher/internal/market"
)

func TestIntradayUpsertPrune(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
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
	sym, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	err = st.UpsertIntraday(ctx, sym.ID, market.Interval15m, []market.IntradayBar{
		{OpenUnix: 100, Open: 1, High: 2, Low: 1, Close: 1.5, Volume: 10},
		{OpenUnix: 200, Open: 1.5, High: 3, Low: 1, Close: 2, Volume: 11},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = st.UpsertIntraday(ctx, sym.ID, market.Interval15m, []market.IntradayBar{
		{OpenUnix: 200, Open: 2, High: 4, Low: 2, Close: 3, Volume: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.IntradayBars(ctx, sym.ID, market.Interval15m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].OpenUnix != 200 || got[1].Close != 3 || got[1].Volume != 12 || got[0].Close != 1.5 {
		t.Fatalf("%+v", got)
	}
	if err := st.PruneIntraday(ctx, sym.ID, market.Interval15m, 150); err != nil {
		t.Fatal(err)
	}
	got, err = st.IntradayBars(ctx, sym.ID, market.Interval15m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OpenUnix != 200 {
		t.Fatalf("pruned %+v", got)
	}
}
