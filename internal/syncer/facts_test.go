package syncer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/store"
)

func TestRefreshFactsKeepsPreviousOnRateLimit(t *testing.T) {
	blockCalendar := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blockCalendar && strings.Contains(r.URL.Path, "/calendar/earnings") {
			http.Error(w, "slow", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/stock/metric"):
			_, _ = w.Write([]byte(`{"metric":{"marketCapitalization":1000,"peTTM":20}}`))
		case strings.Contains(r.URL.Path, "/calendar/earnings"):
			_, _ = w.Write([]byte(`{"earningsCalendar":[{"date":"2026-03-01","hour":"bmo"}]}`))
		default:
			_, _ = w.Write([]byte(`{"data":[
				{"endDate":"2025-03-30","filedDate":"2025-05-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":10},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":1}]}},
				{"endDate":"2025-06-30","filedDate":"2025-08-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":20},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":2}]}},
				{"endDate":"2025-09-30","filedDate":"2025-11-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":30},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":3}]}},
				{"endDate":"2025-12-30","filedDate":"2026-02-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":40},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":4}]}}
			]}`))
		}
	}))
	defer srv.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "MSFT", ProviderSymbol: "MSFT", Name: "Microsoft"})
	if err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{Key: "k", Base: srv.URL, HTTP: srv.Client()}, nil, nil, nil)
	now := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	sy.Now = func() time.Time { return now }

	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}
	row := boardRow(t, st, "AAPL")
	if row.MarketCap == nil || *row.MarketCap != 1000*1e6 || row.PeTTM == nil || *row.PeTTM != 20 || row.RevenueTTM == nil || *row.RevenueTTM != 100 || row.EarningsTTM == nil || *row.EarningsTTM != 10 || row.EarningsDate != "2026-03-01" || row.EarningsHour != "bmo" {
		t.Fatalf("%+v", row)
	}

	blockCalendar = true
	if err := sy.RefreshFacts(ctx, sym.ID); !errors.Is(err, finnhub.ErrRateLimit) {
		t.Fatal(err)
	}
	blockCalendar = false
	row = boardRow(t, st, "AAPL")
	if row.MarketCap == nil || *row.MarketCap != 1000*1e6 || row.EarningsDate != "2026-03-01" {
		t.Fatalf("overwritten %+v", row)
	}

	n, err := sy.refreshStaleFact(ctx, []store.TickSymbol{
		{ID: sym.ID, AssetClass: market.Stock},
		{ID: other.ID, AssetClass: market.Stock},
	})
	if err != nil || n != 1 {
		t.Fatal(err, n)
	}
	if boardRow(t, st, "MSFT").MarketCap == nil {
		t.Fatal("second stock was not filled")
	}
	n, err = sy.refreshStaleFact(ctx, []store.TickSymbol{{ID: sym.ID, AssetClass: market.Stock}, {ID: other.ID, AssetClass: market.Stock}})
	if err != nil || n != 0 {
		t.Fatal(err, n)
	}
}

func openWatch(t *testing.T) (*store.Store, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	board, err := st.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range board.Groups {
		if g.Name == "Watch" {
			return st, g.ID
		}
	}
	t.Fatal("missing watch")
	return nil, 0
}

func boardRow(t *testing.T, st *store.Store, display string) store.Row {
	t.Helper()
	board, err := st.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range board.Groups {
		if g.Name != "Watch" {
			continue
		}
		for _, row := range g.Rows {
			if row.Display == display {
				return row
			}
		}
	}
	t.Fatalf("missing %s", display)
	return store.Row{}
}
