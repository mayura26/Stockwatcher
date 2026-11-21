package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/store"
)

func TestEnsureLogo(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	profiles := 0
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/stock/profile2"):
			profiles++
			if profiles > 8 {
				http.Error(w, "slow", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("symbol") {
			case "NONE":
				_, _ = w.Write([]byte(`{"logo":""}`))
			case "HTML":
				fmt.Fprintf(w, `{"logo":"%s/a.html"}`, srv.URL)
			default:
				fmt.Fprintf(w, `{"logo":"%s/a.png"}`, srv.URL)
			}
		case r.URL.Path == "/a.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case r.URL.Path == "/a.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	add := func(class, display, provider, name string) store.Symbol {
		t.Helper()
		sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: class, Display: display, ProviderSymbol: provider, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return sym
	}
	future := add(market.Future, "ES", "ES=F", "E-mini")
	apple := add(market.Stock, "AAPL", "AAPL", "Apple")
	none := add(market.Stock, "NONE", "NONE", "No logo")
	html := add(market.Stock, "HTML", "HTML", "Bad type")
	msft := add(market.Stock, "MSFT", "MSFT", "Microsoft")
	nvda := add(market.Stock, "NVDA", "NVDA", "Nvidia")
	amd := add(market.Stock, "AMD", "AMD", "AMD")
	coin := add(market.Crypto, "NO LOGO", "NOLOGOUSDT", "No logo")

	sy := New(st, &finnhub.Client{Key: "k", Base: srv.URL, HTTP: srv.Client()}, nil, nil, nil)
	sy.HTTP = srv.Client()
	sy.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	if err := sy.EnsureLogo(ctx, future.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, future.ID); err != store.ErrNotFound {
		t.Fatalf("future logo %v", err)
	}
	if err := sy.EnsureLogo(ctx, coin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, coin.ID); err != store.ErrNotFound {
		t.Fatalf("crypto miss %v", err)
	}
	if profiles != 0 {
		t.Fatalf("future or crypto called finnhub %d", profiles)
	}

	if err := sy.EnsureLogo(ctx, apple.ID); err != nil {
		t.Fatal(err)
	}
	logo, err := st.Logo(ctx, apple.ID)
	if err != nil || logo.MediaType != "image/png" || string(logo.Body) != string(png) {
		t.Fatalf("%s %d %v", logo.MediaType, len(logo.Body), err)
	}
	if !boardRow(t, st, "AAPL").HasLogo {
		t.Fatal("board missing has_logo")
	}

	if err := sy.EnsureLogo(ctx, none.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, none.ID); err != store.ErrNotFound {
		t.Fatalf("empty profile %v", err)
	}
	if err := sy.EnsureLogo(ctx, html.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, html.ID); err != store.ErrNotFound {
		t.Fatalf("html stored %v", err)
	}

	before := profiles
	n, err := sy.refreshLogos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != logoStockBudget {
		t.Fatalf("refreshed %d", n)
	}
	if profiles-before != logoStockBudget {
		t.Fatalf("profile calls %d", profiles-before)
	}
	if _, err := st.Logo(ctx, msft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, nvda.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, amd.ID); err != store.ErrNotFound {
		t.Fatal("third stock fetched inside the cap")
	}

	profiles = 100
	if err := sy.EnsureLogo(ctx, amd.ID); !errors.Is(err, finnhub.ErrRateLimit) {
		t.Fatalf("rate limit %v", err)
	}
	if _, err := st.Logo(ctx, amd.ID); err != store.ErrNotFound {
		t.Fatal("rate limit stored a miss")
	}
}
