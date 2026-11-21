package syncer

import (
	"testing"

	"stockwatcher/internal/store"
)

func stocks(ids ...int64) []store.TickSymbol {
	var out []store.TickSymbol
	for _, id := range ids {
		out = append(out, store.TickSymbol{ID: id, AssetClass: "stock"})
	}
	return out
}

func TestSelectStocksRotates(t *testing.T) {
	in := stocks(1, 2, 3, 4, 5)
	in[0].Armed = true
	got, cursor := SelectStocks(in, 0, 3)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 || cursor != 3 {
		t.Fatalf("got %v cursor %d", ids(got), cursor)
	}
	got, cursor = SelectStocks(in, cursor, 3)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 4 || got[2].ID != 5 || cursor != 5 {
		t.Fatalf("second %v cursor %d", ids(got), cursor)
	}
	got, cursor = SelectStocks(in, cursor, 3)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 || cursor != 3 {
		t.Fatalf("wrap %v cursor %d", ids(got), cursor)
	}
}

func TestSelectStocksKeepsArmedOverCap(t *testing.T) {
	in := stocks(1, 2, 3)
	for i := range in {
		in[i].Armed = true
	}
	got, _ := SelectStocks(in, 0, 2)
	if len(got) != 3 {
		t.Fatalf("armed %v", ids(got))
	}
}

func ids(s []store.TickSymbol) []int64 {
	out := make([]int64, len(s))
	for i := range s {
		out[i] = s[i].ID
	}
	return out
}
