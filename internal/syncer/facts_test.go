package syncer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
)

func TestRefreshFactsKeepsPreviousOnRateLimit(t *testing.T) {
	block := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block {
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

	block = true
	if err := sy.RefreshFacts(ctx, sym.ID); !errors.Is(err, finnhub.ErrRateLimit) {
		t.Fatal(err)
	}
	block = false
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

func TestRefreshFactsFillsGapsFromYahoo(t *testing.T) {
	complete := false
	fh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/stock/metric"):
			_, _ = w.Write([]byte(`{"metric":{"marketCapitalization":1000,"peTTM":20}}`))
		case strings.Contains(r.URL.Path, "/calendar/earnings"):
			_, _ = w.Write([]byte(`{"earningsCalendar":[{"date":"2026-03-01","hour":"bmo"}]}`))
		case complete:
			_, _ = w.Write([]byte(reportedQuarters(10, 1)))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer fh.Close()
	yahooHits := 0
	yh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		yahooHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(yahooFactsJSON(9e9, 99, 50, -4, "2026-05-01")))
	}))
	defer yh.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "NBIS", ProviderSymbol: "NBIS", Name: "Nebius"})
	if err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{Key: "k", Base: fh.URL, HTTP: fh.Client()}, &yahoo.Client{Base: yh.URL, HTTP: yh.Client()}, nil, nil)
	sy.Now = func() time.Time { return time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC) }

	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}
	row := boardRow(t, st, "NBIS")
	if row.MarketCap == nil || *row.MarketCap != 1000*1e6 || row.PeTTM == nil || *row.PeTTM != 20 || row.RevenueTTM == nil || *row.RevenueTTM != 50 || row.EarningsTTM == nil || *row.EarningsTTM != -4 || row.EarningsDate != "2026-03-01" || row.EarningsHour != "bmo" {
		t.Fatalf("%+v", row)
	}
	if yahooHits != 1 {
		t.Fatalf("yahoo hits %d", yahooHits)
	}

	complete = true
	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}
	row = boardRow(t, st, "NBIS")
	if row.RevenueTTM == nil || *row.RevenueTTM != 100 || row.EarningsTTM == nil || *row.EarningsTTM != 10 || yahooHits != 1 {
		t.Fatalf("%+v hits %d", row, yahooHits)
	}
}

func TestRefreshFactsYahooOnRateLimit(t *testing.T) {
	fh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow", http.StatusTooManyRequests)
	}))
	defer fh.Close()
	yh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(yahooFactsJSON(5000, 8, 12, -2, "2026-04-01")))
	}))
	defer yh.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "NVDA", ProviderSymbol: "NVDA", Name: "NVIDIA"})
	if err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{Key: "k", Base: fh.URL, HTTP: fh.Client()}, &yahoo.Client{Base: yh.URL, HTTP: yh.Client()}, nil, nil)
	sy.Now = func() time.Time { return time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC) }
	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}
	row := boardRow(t, st, "NVDA")
	if row.MarketCap == nil || *row.MarketCap != 5000 || row.PeTTM == nil || *row.PeTTM != 8 || row.RevenueTTM == nil || *row.RevenueTTM != 12 || row.EarningsTTM == nil || *row.EarningsTTM != -2 || row.EarningsDate != "2026-04-01" || row.EarningsHour != "" {
		t.Fatalf("%+v", row)
	}
}

func TestRefreshFactsKeepsStoredFieldWhenBothFail(t *testing.T) {
	failMetric := false
	fh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMetric && strings.Contains(r.URL.Path, "/stock/metric") {
			http.Error(w, "slow", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/stock/metric"):
			_, _ = w.Write([]byte(`{"metric":{"marketCapitalization":1000,"peTTM":20}}`))
		case strings.Contains(r.URL.Path, "/calendar/earnings"):
			date := "2026-03-01"
			if failMetric {
				date = "2026-04-02"
			}
			_, _ = w.Write([]byte(`{"earningsCalendar":[{"date":"` + date + `","hour":"amc"}]}`))
		default:
			step := 10.0
			if failMetric {
				step = 100
			}
			_, _ = w.Write([]byte(reportedQuarters(step, step/10)))
		}
	}))
	defer fh.Close()
	yh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer yh.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "AMD", ProviderSymbol: "AMD", Name: "AMD"})
	if err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{Key: "k", Base: fh.URL, HTTP: fh.Client()}, &yahoo.Client{Base: yh.URL, HTTP: yh.Client()}, nil, nil)
	sy.Now = func() time.Time { return time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC) }
	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}

	failMetric = true
	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}
	row := boardRow(t, st, "AMD")
	if row.MarketCap == nil || *row.MarketCap != 1000*1e6 || row.PeTTM == nil || *row.PeTTM != 20 || row.RevenueTTM == nil || *row.RevenueTTM != 1000 || row.EarningsTTM == nil || *row.EarningsTTM != 100 || row.EarningsDate != "2026-04-02" || row.EarningsHour != "amc" {
		t.Fatalf("%+v", row)
	}
}

func TestRefreshFactsYahooListed(t *testing.T) {
	finn := 0
	fh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finn++
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer fh.Close()
	yh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(yahooFactsJSON(1234, 0, 10, 1, "2026-02-03")))
	}))
	defer yh.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "005930.KS", ProviderSymbol: "005930.KS", Name: "Samsung"})
	if err != nil {
		t.Fatal(err)
	}
	sy := New(st, &finnhub.Client{Key: "k", Base: fh.URL, HTTP: fh.Client()}, &yahoo.Client{Base: yh.URL, HTTP: yh.Client()}, nil, nil)
	sy.Now = func() time.Time { return time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC) }
	if err := sy.RefreshFacts(ctx, sym.ID); err != nil {
		t.Fatal(err)
	}
	if finn != 0 {
		t.Fatalf("finnhub calls %d", finn)
	}
	row := boardRow(t, st, "005930.KS")
	if row.MarketCap == nil || *row.MarketCap != 1234 || row.PeTTM != nil || row.RevenueTTM == nil || *row.RevenueTTM != 10 || row.EarningsDate != "2026-02-03" || row.EarningsHour != "" {
		t.Fatalf("%+v", row)
	}
}

func reportedQuarters(rev, earn float64) string {
	return `{"data":[
		{"endDate":"2025-03-30","filedDate":"2025-05-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":` + num(rev) + `},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":` + num(earn) + `}]}},
		{"endDate":"2025-06-30","filedDate":"2025-08-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":` + num(rev*2) + `},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":` + num(earn*2) + `}]}},
		{"endDate":"2025-09-30","filedDate":"2025-11-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":` + num(rev*3) + `},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":` + num(earn*3) + `}]}},
		{"endDate":"2025-12-30","filedDate":"2026-02-01","report":{"ic":[{"concept":"us-gaap_Revenues","unit":"usd","value":` + num(rev*4) + `},{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":` + num(earn*4) + `}]}}
	]}`
}

func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func yahooFactsJSON(cap, pe, rev, earn float64, date string) string {
	return `{"quoteSummary":{"result":[{"price":{"marketCap":{"raw":` + num(cap) + `}},"summaryDetail":{"trailingPE":{"raw":` + num(pe) + `}},"financialData":{"totalRevenue":{"raw":` + num(rev) + `}},"defaultKeyStatistics":{"netIncomeToCommon":{"raw":` + num(earn) + `}},"calendarEvents":{"earnings":{"earningsDate":[{"fmt":"` + date + `"}]}}}],"error":null}}`
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
