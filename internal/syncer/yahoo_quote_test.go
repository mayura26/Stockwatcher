package syncer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
)

func TestYahooStockQuoteSkipsFinnhub(t *testing.T) {
	var mu sync.Mutex
	var finnhubSyms []string
	fh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		finnhubSyms = append(finnhubSyms, r.URL.Query().Get("symbol"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"c":10,"pc":9,"dp":11.1,"t":1700000000}`))
	}))
	defer fh.Close()

	var yahooSyms []string
	yh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		yahooSyms = append(yahooSyms, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":78000,"previousClose":77000,"regularMarketTime":1700000000,"symbol":"005930.KS","regularMarketChangePercent":1.3},"timestamp":[1700000000],"indicators":{"quote":[{"open":[77000],"high":[78500],"low":[76000],"close":[78000],"volume":[10]}],"adjclose":[{"adjclose":[78000]}]}}],"error":null}}`))
	}))
	defer yh.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	ks, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "005930.KS", ProviderSymbol: "005930.KS", Name: "Samsung Electronics"})
	if err != nil {
		t.Fatal(err)
	}
	aapl, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	sy := New(st,
		&finnhub.Client{Key: "k", Base: fh.URL, HTTP: http.DefaultClient},
		&yahoo.Client{Base: yh.URL, HTTP: http.DefaultClient},
		nil, nil)

	n, err := sy.refreshStaleFact(ctx, []store.TickSymbol{
		{ID: ks.ID, AssetClass: market.Stock, ProviderSymbol: "005930.KS"},
	})
	if err != nil || n != 0 {
		t.Fatal(err, n)
	}

	quotes, touched, err := sy.refreshQuotes(ctx, []store.TickSymbol{
		{ID: ks.ID, AssetClass: market.Stock, Display: "005930.KS", ProviderSymbol: "005930.KS"},
		{ID: aapl.ID, AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if quotes[ks.ID].Price != 78000 {
		t.Fatalf("ks quote %+v", quotes[ks.ID])
	}
	if !touched[ks.ID] {
		t.Fatal("ks bars were not stored")
	}
	bars, err := st.Bars(ctx, ks.ID)
	if err != nil || len(bars) == 0 || bars[0].Close != 78000 {
		t.Fatalf("bars %+v %v", bars, err)
	}
	got, err := sy.fetchBars(ctx, market.Stock, "005930.KS", false)
	if err != nil || len(got) == 0 {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, sym := range finnhubSyms {
		if sym != "AAPL" {
			t.Fatalf("finnhub saw %q in %v", sym, finnhubSyms)
		}
	}
	if len(finnhubSyms) == 0 {
		t.Fatal("AAPL was not quoted")
	}
	sawKS := false
	for _, path := range yahooSyms {
		if len(path) >= 9 && path[len(path)-9:] == "005930.KS" {
			sawKS = true
		}
	}
	if !sawKS {
		t.Fatalf("yahoo paths %v", yahooSyms)
	}
}
