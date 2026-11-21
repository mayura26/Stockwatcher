package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"stockwatcher/internal/config"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
)

func TestStockSearchFallsBackToYahoo(t *testing.T) {
	yahooHits := 0
	yahooSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		yahooHits++
		if r.URL.Path != "/v1/finance/search" {
			t.Errorf("yahoo path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"quotes":[
			{"symbol":"005930.KS","longname":"Samsung Electronics Co., Ltd.","quoteType":"EQUITY","exchDisp":"KSE"},
			{"symbol":"000660.KS","longname":"SK hynix Inc.","quoteType":"EQUITY","exchDisp":"KSE"},
			{"symbol":"ES=F","quoteType":"FUTURE","shortname":"E-mini"}
		]}`))
	}))
	defer yahooSrv.Close()

	finnhubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"count":0,"result":[]}`))
	}))
	defer finnhubSrv.Close()

	h := searchHandler(t, finnhubSrv.URL, yahooSrv.URL)
	body := getSearch(t, h, "samsung")
	if yahooHits != 1 {
		t.Fatalf("yahoo calls %d", yahooHits)
	}
	if len(body.Hits) != 2 || body.Hits[0].ProviderSymbol != "005930.KS" || body.Hits[1].ProviderSymbol != "000660.KS" {
		t.Fatalf("%+v", body.Hits)
	}
	for _, hit := range body.Hits {
		if hit.ProviderSymbol == "SAMSUNG" {
			t.Fatalf("fake ticker %+v", body.Hits)
		}
	}
}

func TestStockSearchSkipsYahooWhenFinnhubHits(t *testing.T) {
	yahooSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("yahoo was called")
	}))
	defer yahooSrv.Close()
	finnhubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"count":1,"result":[{"description":"APPLE INC","displaySymbol":"AAPL","symbol":"AAPL","type":"Common Stock"}]}`))
	}))
	defer finnhubSrv.Close()

	body := getSearch(t, searchHandler(t, finnhubSrv.URL, yahooSrv.URL), "aapl")
	if len(body.Hits) != 1 || body.Hits[0].ProviderSymbol != "AAPL" {
		t.Fatalf("%+v", body.Hits)
	}
}

func TestStockSearchYahooErrorDoesNotInventTicker(t *testing.T) {
	finnhubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"count":0,"result":[]}`))
	}))
	defer finnhubSrv.Close()
	yahooSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow", http.StatusTooManyRequests)
	}))
	defer yahooSrv.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/search?class=stock&q=samsung", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	searchHandler(t, finnhubSrv.URL, yahooSrv.URL).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SAMSUNG") {
		t.Fatalf("invented ticker %s", rec.Body.String())
	}
}

func TestStockSearchManualTickerWhenBothEmpty(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/finance/search" {
			_, _ = w.Write([]byte(`{"quotes":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"count":0,"result":[]}`))
	}))
	defer empty.Close()

	body := getSearch(t, searchHandler(t, empty.URL, empty.URL), "AAPL")
	if len(body.Hits) != 1 || body.Hits[0].ProviderSymbol != "AAPL" || body.Hits[0].Name != "US stock" {
		t.Fatalf("%+v", body.Hits)
	}
}

func TestIndexSearch(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("search left the process")
	}))
	defer empty.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/search?class=index&q=tnx", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	searchHandler(t, empty.URL, empty.URL).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Hits []struct {
			AssetClass     string `json:"asset_class"`
			Display        string `json:"display"`
			ProviderSymbol string `json:"provider_symbol"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hits) != 1 || body.Hits[0].ProviderSymbol != "^TNX" || body.Hits[0].Display != "TNX" || body.Hits[0].AssetClass != "stock" {
		t.Fatalf("%+v", body.Hits)
	}
}

func searchHandler(t *testing.T, finnhubURL, yahooURL string) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sy := syncer.New(st,
		&finnhub.Client{Key: "k", Base: finnhubURL, HTTP: http.DefaultClient},
		&yahoo.Client{Base: yahooURL, HTTP: http.DefaultClient},
		nil, nil)
	return New(st, sy, config.Config{AccessToken: "testtoken"}).Handler()
}

func getSearch(t *testing.T, h http.Handler, q string) struct {
	Hits []struct {
		ProviderSymbol string `json:"provider_symbol"`
		Name           string `json:"name"`
	} `json:"hits"`
} {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/search?class=stock&q="+q, nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Hits []struct {
			ProviderSymbol string `json:"provider_symbol"`
			Name           string `json:"name"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
