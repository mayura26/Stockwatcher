package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stockwatcher/internal/market"
)

func TestBoardGroupsAndLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "has space")
	st, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Groups) != 1 || board.Groups[0].Name != "Watch" {
		t.Fatalf("%+v", board.Groups)
	}
	gid := board.Groups[0].ID
	sym, created, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Future, Display: "ES", ProviderSymbol: "ES=F", Name: "E-mini S&P 500"})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	day := 1.25
	if err := st.UpsertQuote(ctx, sym.ID, 5800, nil, &day, nil, nil, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertBars(ctx, sym.ID, []market.Bar{{Day: "2024-01-02", Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 3}}); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Groups[0].Rows) != 1 || board.Groups[0].Rows[0].Display != "ES" || board.Groups[0].Rows[0].Price == nil || *board.Groups[0].Rows[0].Price != 5800 {
		t.Fatalf("%+v", board.Groups[0].Rows)
	}
	ok, err := st.TryLock(ctx, "tick", time.Minute)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	ok, err = st.TryLock(ctx, "tick", time.Minute)
	if err != nil || ok {
		t.Fatal("second lock", err, ok)
	}
	if err := st.Unlock(ctx, "tick"); err != nil {
		t.Fatal(err)
	}
	ok, err = st.TryLock(ctx, "tick", time.Minute)
	if err != nil || !ok {
		t.Fatal("relock", err, ok)
	}
	if err := st.DeleteGroup(ctx, gid); err != ErrLastGroup {
		t.Fatal(err)
	}
}
