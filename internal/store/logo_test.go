package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"stockwatcher/internal/market"
)

func TestLogoStorage(t *testing.T) {
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
	gid := groupNamed(t, board, "Watch").ID
	sym, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Future, Display: "ES", ProviderSymbol: "ES=F", Name: "E-mini"})
	if err != nil {
		t.Fatal(err)
	}

	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if row := watchRow(t, board, "AAPL"); row.HasLogo {
		t.Fatal("logo before fetch")
	}
	if _, err := st.Logo(ctx, sym.ID); err != ErrNotFound {
		t.Fatal(err)
	}
	need, err := st.SymbolsNeedingLogo(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(need) != 2 || need[0].ID != sym.ID || need[1].ID != other.ID {
		t.Fatalf("%+v", need)
	}

	body := []byte{0x89, 'P', 'N', 'G'}
	if err := st.PutLogo(ctx, sym.ID, body, "image/png", 50); err != nil {
		t.Fatal(err)
	}
	logo, err := st.Logo(ctx, sym.ID)
	if err != nil || logo.MediaType != "image/png" || !bytes.Equal(logo.Body, body) {
		t.Fatalf("%+v %v", logo, err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if row := watchRow(t, board, "AAPL"); !row.HasLogo {
		t.Fatal("has_logo not set")
	}

	if err := st.PutLogo(ctx, sym.ID, nil, "", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, sym.ID); err != ErrNotFound {
		t.Fatalf("miss returned %v", err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if row := watchRow(t, board, "AAPL"); row.HasLogo {
		t.Fatal("miss still flagged")
	}
	need, err = st.SymbolsNeedingLogo(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(need) != 1 || need[0].ID != other.ID {
		t.Fatalf("fresh miss retried: %+v", need)
	}
	need, err = st.SymbolsNeedingLogo(ctx, 101)
	if err != nil {
		t.Fatal(err)
	}
	if len(need) != 2 {
		t.Fatalf("old miss not due: %+v", need)
	}
}

func watchRow(t *testing.T, board Board, display string) Row {
	t.Helper()
	for _, row := range groupNamed(t, board, "Watch").Rows {
		if row.Display == display {
			return row
		}
	}
	t.Fatalf("missing %s", display)
	return Row{}
}
