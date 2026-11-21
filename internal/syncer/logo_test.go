package syncer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/store"
)

func TestEnsureLogo(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	var requests int
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		name := strings.TrimPrefix(r.URL.Path, "/stock_logo/")
		switch name {
		case "NONE.png":
			http.NotFound(w, r)
		case "HTML.png":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html></html>"))
		default:
			if !strings.HasSuffix(name, ".png") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
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
	index := add(market.Stock, "TNX", "^TNX", "10-Year Treasury Yield")
	apple := add(market.Stock, "AAPL", "AAPL", "Apple")
	none := add(market.Stock, "NONE", "NONE", "No logo")
	html := add(market.Stock, "HTML", "HTML", "Bad type")
	msft := add(market.Stock, "MSFT", "MSFT", "Microsoft")
	nvda := add(market.Stock, "NVDA", "NVDA", "Nvidia")
	amd := add(market.Stock, "AMD", "AMD", "AMD")
	coin := add(market.Crypto, "NO LOGO", "NOLOGOUSDT", "No logo")

	sy := New(st, nil, nil, nil, nil)
	sy.HTTP = srv.Client()
	sy.LogoBase = srv.URL + "/stock_logo/"
	sy.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	if err := sy.EnsureLogo(ctx, future.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, future.ID); err != store.ErrNotFound {
		t.Fatalf("future logo %v", err)
	}
	if err := sy.EnsureLogo(ctx, index.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, index.ID); err != store.ErrNotFound {
		t.Fatalf("index logo %v", err)
	}
	if requests != 0 {
		t.Fatal("index logo was fetched")
	}
	if err := sy.EnsureLogo(ctx, coin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, coin.ID); err != store.ErrNotFound {
		t.Fatalf("crypto miss %v", err)
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
		t.Fatalf("missing file %v", err)
	}
	if err := sy.EnsureLogo(ctx, html.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Logo(ctx, html.ID); err != store.ErrNotFound {
		t.Fatalf("html stored %v", err)
	}

	before := requests
	if err := sy.EnsureLogo(ctx, apple.ID); err != nil {
		t.Fatal(err)
	}
	if requests != before {
		t.Fatal("stored logo was fetched again")
	}

	n, err := sy.refreshLogos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("refreshed %d", n)
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
	n, err = sy.refreshLogos(ctx)
	if err != nil || n != 1 {
		t.Fatalf("second pass %d %v", n, err)
	}
	if _, err := st.Logo(ctx, amd.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLogoMissRetryAndMonthlySweep(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1}
	next := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 2}
	var requests int
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		name := strings.TrimPrefix(r.URL.Path, "/stock_logo/")
		switch name {
		case "NONE.png", "KEEP.png":
			http.NotFound(w, r)
		case "NEW.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(next)
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		}
	}))
	defer srv.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	add := func(display, provider string) store.Symbol {
		t.Helper()
		sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: display, ProviderSymbol: provider, Name: display})
		if err != nil {
			t.Fatal(err)
		}
		return sym
	}
	none := add("NONE", "NONE")
	now := time.Unix(1_700_000_000, 0)
	sy := New(st, nil, nil, nil, nil)
	sy.HTTP = srv.Client()
	sy.LogoBase = srv.URL + "/stock_logo/"
	sy.Now = func() time.Time { return now }

	if err := st.PutLogo(ctx, none.ID, nil, "", now.Add(-8*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := sy.refreshLogos(ctx); err != nil {
		t.Fatal(err)
	}
	if requests == 0 {
		t.Fatal("old miss was not tried")
	}
	if _, err := st.Logo(ctx, none.ID); err != store.ErrNotFound {
		t.Fatal("old miss should stay empty when the file is still missing")
	}
	requests = 0
	if _, err := sy.refreshLogos(ctx); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatal("fresh miss was tried again")
	}

	fresh := add("NEW", "NEW")
	keep := add("KEEP", "KEEP")
	if err := st.PutLogo(ctx, fresh.ID, png, "image/png", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := st.PutLogo(ctx, keep.ID, png, "image/png", now.Unix()); err != nil {
		t.Fatal(err)
	}
	later := now.Add(40 * 24 * time.Hour)
	if err := st.PutLogo(ctx, none.ID, nil, "", later.Unix()); err != nil {
		t.Fatal(err)
	}
	sy.Now = func() time.Time { return later }
	if _, err := sy.refreshLogos(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.Logo(ctx, fresh.ID)
	if err != nil || string(got.Body) != string(next) {
		t.Fatalf("replaced %+v %v", got.Body, err)
	}
	kept, err := st.Logo(ctx, keep.ID)
	if err != nil || string(kept.Body) != string(png) {
		t.Fatalf("failed fetch replaced the stored image %+v %v", kept.Body, err)
	}
}
