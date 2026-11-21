package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"log/slog"

	"stockwatcher/internal/market"
	"stockwatcher/internal/store"
)

const (
	logoStockBudget  = 2
	logoCryptoBudget = 8
	logoRecheck      = 30 * 24 * time.Hour
)

const (
	metaLogosChecked = "logos_checked_at"
	metaLogosSweep   = "logos_sweep_at"
)

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

// EnsureLogo fetches one mark and stores it. A stored image is left alone.
// Futures are recorded as a miss without a request.
func (s *Syncer) EnsureLogo(ctx context.Context, id int64) error {
	if ok, err := s.Store.HasLogo(ctx, id); err != nil {
		return err
	} else if ok {
		return nil
	}
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
	body, media, err := s.pullStockLogo(ctx, sym)
	if err != nil {
		if errors.Is(err, errLogoMiss) {
			return s.Store.PutLogo(ctx, sym.ID, nil, "", s.now().Unix())
		}
		return err
	}
	return s.Store.PutLogo(ctx, sym.ID, body, media, s.now().Unix())
}

func (s *Syncer) pullStockLogo(ctx context.Context, sym store.Symbol) ([]byte, string, error) {
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
		return nil, "", errLogoMiss
	}
	var last error
	for _, raw := range urls {
		body, media, err := s.fetchLogo(ctx, raw)
		if err == nil {
			return body, media, nil
		}
		if !errors.Is(err, errLogoMiss) {
			return nil, "", err
		}
		last = err
	}
	if last == nil {
		last = errLogoMiss
	}
	return nil, "", last
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

// refreshLogos fills misses, then continues a monthly recheck of stored images.
// Stocks stay at two fetches so a long list does not stall the tick. Crypto is
// capped the same way. One failure does not stop the rest. A stored image is
// not fetched again until that monthly recheck.
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
	stockLeft := logoStockBudget
	cryptoLeft := logoCryptoBudget
	if err := s.writeLogos(ctx, futures, len(futures), &n); err != nil {
		return n, err
	}
	if err := s.writeLogos(ctx, cryptos, cryptoLeft, &n); err != nil {
		return n, err
	}
	cryptoLeft -= len(cryptos)
	if cryptoLeft < 0 {
		cryptoLeft = 0
	}
	if err := s.writeLogos(ctx, stocks, stockLeft, &n); err != nil {
		return n, err
	}
	stockLeft -= len(stocks)
	if stockLeft < 0 {
		stockLeft = 0
	}
	checked, err := s.recheckStoredLogos(ctx, stockLeft, cryptoLeft)
	if err != nil {
		return n, err
	}
	return n + checked, nil
}

func (s *Syncer) writeLogos(ctx context.Context, list []store.Symbol, budget int, n *int) error {
	if budget > len(list) {
		budget = len(list)
	}
	if budget < 0 {
		budget = 0
	}
	for _, sym := range list[:budget] {
		if err := s.EnsureLogo(ctx, sym.ID); err != nil {
			slog.Error("logo", "symbol", sym.ID, "err", err)
			continue
		}
		*n++
	}
	return nil
}

// recheckStoredLogos refetches saved images about once a month.
// Startup and the tick share the stamp. A failed fetch leaves the stored image.
// The stamp is written only after every saved image has been visited, so a
// restart continues the same sweep.
func (s *Syncer) recheckStoredLogos(ctx context.Context, stockLeft, cryptoLeft int) (int, error) {
	sweepAt, active, err := s.logoSweep(ctx)
	if err != nil || !active {
		return 0, err
	}
	due, err := s.Store.StoredLogosBefore(ctx, sweepAt)
	if err != nil {
		return 0, err
	}
	var stocks, cryptos []store.Symbol
	for _, sym := range due {
		switch sym.AssetClass {
		case market.Stock:
			stocks = append(stocks, sym)
		case market.Crypto:
			cryptos = append(cryptos, sym)
		}
	}
	n := 0
	visit := func(list []store.Symbol, budget int) {
		if budget > len(list) {
			budget = len(list)
		}
		if budget < 0 {
			budget = 0
		}
		for _, sym := range list[:budget] {
			if err := s.recheckLogo(ctx, sym); err != nil {
				slog.Error("logo", "symbol", sym.ID, "err", err)
				continue
			}
			n++
		}
	}
	visit(stocks, stockLeft)
	visit(cryptos, cryptoLeft)
	left, err := s.Store.StoredLogosBefore(ctx, sweepAt)
	if err != nil {
		return n, err
	}
	if len(left) == 0 {
		if err := s.Store.SetMeta(ctx, metaLogosChecked, strconv.FormatInt(s.now().Unix(), 10)); err != nil {
			return n, err
		}
		if err := s.Store.SetMeta(ctx, metaLogosSweep, ""); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (s *Syncer) logoSweep(ctx context.Context) (int64, bool, error) {
	raw, ok, err := s.Store.Meta(ctx, metaLogosSweep)
	if err != nil {
		return 0, false, err
	}
	if ok && raw != "" && raw != "0" {
		at, conv := strconv.ParseInt(raw, 10, 64)
		if conv == nil && at > 0 {
			return at, true, nil
		}
	}
	checked, ok, err := s.Store.Meta(ctx, metaLogosChecked)
	if err != nil {
		return 0, false, err
	}
	if ok && checked != "" {
		at, conv := strconv.ParseInt(checked, 10, 64)
		if conv == nil && s.now().Sub(time.Unix(at, 0)) < logoRecheck {
			return 0, false, nil
		}
	}
	at := s.now().Unix()
	if err := s.Store.SetMeta(ctx, metaLogosSweep, strconv.FormatInt(at, 10)); err != nil {
		return 0, false, err
	}
	return at, true, nil
}

func (s *Syncer) recheckLogo(ctx context.Context, sym store.Symbol) error {
	var body []byte
	var media string
	var err error
	switch sym.AssetClass {
	case market.Stock:
		body, media, err = s.pullStockLogo(ctx, sym)
	case market.Crypto:
		body, media, err = s.pullCryptoLogo(ctx, sym)
	default:
		return s.Store.TouchLogo(ctx, sym.ID, s.now().Unix())
	}
	if err != nil {
		if errors.Is(err, errLogoMiss) {
			return s.Store.TouchLogo(ctx, sym.ID, s.now().Unix())
		}
		return err
	}
	return s.Store.PutLogo(ctx, sym.ID, body, media, s.now().Unix())
}

func (s *Syncer) pullCryptoLogo(ctx context.Context, sym store.Symbol) ([]byte, string, error) {
	raw, ok := market.CryptoLogoURL(sym.Display)
	if !ok {
		return nil, "", errLogoMiss
	}
	return s.fetchLogo(ctx, raw)
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
