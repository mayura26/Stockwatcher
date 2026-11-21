package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"stockwatcher/internal/config"
	"stockwatcher/internal/market"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
)

func TestLogoRoute(t *testing.T) {
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
		if !g.Synthetic {
			gid = g.ID
		}
	}
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0x89, 'P', 'N', 'G'}
	if err := st.PutLogo(ctx, sym.ID, body, "image/png", 1); err != nil {
		t.Fatal(err)
	}
	h := New(st, syncer.New(st, nil, nil, nil, nil), config.Config{AccessToken: "testtoken"}).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/symbols/"+strconv.FormatInt(sym.ID, 10)+"/logo", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/symbols/"+strconv.FormatInt(sym.ID, 10)+"/logo", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.String() != string(body) {
		t.Fatalf("%d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/symbols/999/logo", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing %d", rec.Code)
	}
}
