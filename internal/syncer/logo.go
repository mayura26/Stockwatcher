package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"log/slog"

	"stockwatcher/internal/market"
	"stockwatcher/internal/store"
)

const logoCryptoBudget = 8

// errLogoMiss is a looked-up symbol with nothing usable to store.
// A network failure is not a miss, so the next tick can try again.
var errLogoMiss = errors.New("logo miss")

var logoClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 3 {
			return errLogoMiss
		}
		return nil
	},
}

func (s *Syncer) httpClient() *http.Client {
	if s != nil && s.HTTP != nil {
		return s.HTTP
	}
	return logoClient
}

// EnsureLogo fetches one mark and stores it. Futures are recorded as a miss
// without a request. A stock with no Finnhub key is left unchecked.
func (s *Syncer) EnsureLogo(ctx context.Context, id int64) error {
	sym, err := s.Store.Symbol(ctx, id)
	if err != nil {
		return err
	}
	switch sym.AssetClass {
	case market.Future:
		return s.Store.PutLogo(ctx, id, nil, "", s.now().Unix())
	case market.Stock:
		return s.ensureStockLogo(ctx, sym)
	case market.Crypto:
		return s.ensureCryptoLogo(ctx, sym)
	default:
		return s.Store.PutLogo(ctx, id, nil, "", s.now().Unix())
	}
}

func (s *Syncer) ensureStockLogo(ctx context.Context, sym store.Symbol) error {
	base := s.LogoBase
	if base == "" {
		base = market.StockLogoBase
	}
	var urls []string
	if raw, ok := market.StockLogoURL(base, sym.ProviderSymbol); ok {
		urls = append(urls, raw)
	}
	// A test host replaces the first URL. The public fallback stays off so the test does not call out.
	if s.LogoBase == "" {
		if alt, ok := market.ParqetLogoURL(sym.ProviderSymbol); ok {
			urls = append(urls, alt)
		}
	}
	if len(urls) == 0 {
		return s.Store.PutLogo(ctx, sym.ID, nil, "", s.now().Unix())
	}
	var last error
	for _, raw := range urls {
		body, media, err := s.fetchLogo(ctx, raw)
		if err == nil {
			return s.Store.PutLogo(ctx, sym.ID, body, media, s.now().Unix())
		}
		if !errors.Is(err, errLogoMiss) {
			return err
		}
		last = err
	}
	if last != nil {
		return s.Store.PutLogo(ctx, sym.ID, nil, "", s.now().Unix())
	}
	return nil
}

func (s *Syncer) ensureCryptoLogo(ctx context.Context, sym store.Symbol) error {
	raw, ok := market.CryptoLogoURL(sym.Display)
	if !ok {
		return s.Store.PutLogo(ctx, sym.ID, nil, "", s.now().Unix())
	}
	return s.storeFetchedLogo(ctx, sym.ID, raw)
}

func (s *Syncer) storeFetchedLogo(ctx context.Context, id int64, raw string) error {
	body, media, err := s.fetchLogo(ctx, raw)
	if err != nil {
		if errors.Is(err, errLogoMiss) {
			return s.Store.PutLogo(ctx, id, nil, "", s.now().Unix())
		}
		return err
	}
	return s.Store.PutLogo(ctx, id, body, media, s.now().Unix())
}

// RefreshLogos fills marks that are missing or due for another try.
func (s *Syncer) RefreshLogos(ctx context.Context) (int, error) {
	return s.refreshLogos(ctx)
}

// refreshLogos fills misses. Futures and stocks are all fetched. Crypto is
// capped so a long list does not stall the tick. One failure does not stop the rest.
func (s *Syncer) refreshLogos(ctx context.Context) (int, error) {
	need, err := s.Store.SymbolsNeedingLogo(ctx, s.now().Add(-7*24*time.Hour).Unix())
	if err != nil {
		return 0, err
	}
	var futures, cryptos, stocks []store.Symbol
	for _, sym := range need {
		switch sym.AssetClass {
		case market.Stock:
			stocks = append(stocks, sym)
		case market.Crypto:
			cryptos = append(cryptos, sym)
		default:
			futures = append(futures, sym)
		}
	}
	n := 0
	write := func(list []store.Symbol, budget int) error {
		if budget > len(list) {
			budget = len(list)
		}
		for _, sym := range list[:budget] {
			if err := s.EnsureLogo(ctx, sym.ID); err != nil {
				slog.Error("logo", "symbol", sym.ID, "err", err)
				continue
			}
			n++
		}
		return nil
	}
	if err := write(futures, len(futures)); err != nil {
		return n, err
	}
	if err := write(cryptos, logoCryptoBudget); err != nil {
		return n, err
	}
	if err := write(stocks, len(stocks)); err != nil {
		return n, err
	}
	return n, nil
}

func (s *Syncer) fetchLogo(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, "", errLogoMiss
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "stockwatcher/1.0")
	res, err := s.httpClient().Do(req)
	if err != nil {
		if errors.Is(err, errLogoMiss) {
			return nil, "", errLogoMiss
		}
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusGone {
		return nil, "", errLogoMiss
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("logo status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, market.MaxLogoBytes+1))
	if err != nil {
		return nil, "", err
	}
	media, err := market.AcceptLogo(res.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, "", errLogoMiss
	}
	return body, media, nil
}
