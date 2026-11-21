package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"stockwatcher/internal/config"
	"stockwatcher/internal/market"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
)

func TestBarsResolution(t *testing.T) {
	yahooSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"chart":{"result":[{"timestamp":[1700000000],"indicators":{"quote":[{"open":[9],"high":[11],"low":[8],"close":[10]}]}}],"error":null}}`))
	}))
	defer yahooSrv.Close()

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
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{
		AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, syncer.New(st, nil, &yahoo.Client{Base: yahooSrv.URL}, nil, nil), config.Config{AccessToken: "testtoken"})
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/bars?id="+strconv.FormatInt(sym.ID, 10)+"&resolution=5m", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Resolution string `json:"resolution"`
		Bars       []struct {
			Time  int64   `json:"time"`
			Close float64 `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Resolution != "5m" || len(body.Bars) != 1 || body.Bars[0].Time != 1700000000 || body.Bars[0].Close != 10 {
		t.Fatalf("%+v", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/bars?id="+strconv.FormatInt(sym.ID, 10)+"&resolution=nope", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}
