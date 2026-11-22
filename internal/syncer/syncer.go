package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"stockwatcher/internal/alerts"
	"stockwatcher/internal/market"
	"stockwatcher/internal/market/binance"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/push"
	"stockwatcher/internal/store"
)

var errStoreBars = errors.New("store bars")

// Syncer refreshes quotes and bars, then evaluates price crosses.
type Syncer struct {
	Store    *store.Store
	Finnhub  *finnhub.Client
	Yahoo    *yahoo.Client
	Binance  *binance.Client
	Push     *push.Sender
	Now      func() time.Time
	HTTP     *http.Client
	LogoBase string

	mu       sync.Mutex
	inflight map[int64]chan struct{}
	spans    map[string]chan struct{}
}

func New(st *store.Store, fh *finnhub.Client, yh *yahoo.Client, bn *binance.Client, ps *push.Sender) *Syncer {
	return &Syncer{
		Store:    st,
		Finnhub:  fh,
		Yahoo:    yh,
		Binance:  bn,
		Push:     ps,
		inflight: map[int64]chan struct{}{},
		spans:    map[string]chan struct{}{},
	}
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Enqueue backfills history and a first quote without blocking the add request.
func (s *Syncer) Enqueue(id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
		defer cancel()
		if err := s.Backfill(ctx, id); err != nil {
			slog.Error("backfill", "symbol", id, "err", err)
		}
		if err := s.RefreshQuote(ctx, id); err != nil {
			slog.Error("quote", "symbol", id, "err", err)
		}
		if err := s.RefreshFacts(ctx, id); err != nil {
			slog.Error("facts", "symbol", id, "err", err)
		}
		if err := s.EnsureLogo(ctx, id); err != nil {
			slog.Error("logo", "symbol", id, "err", err)
		}
		if err := s.warmIntraday(ctx, id); err != nil {
			slog.Error("intraday cache", "symbol", id, "err", err)
		}
	}()
}

// RefreshQuote fetches one symbol and stores its price plus D/W/M and, when intraday loads, 1H/4H.
func (s *Syncer) RefreshQuote(ctx context.Context, id int64) error {
	sym, err := s.Store.Symbol(ctx, id)
	if err != nil {
		return err
	}
	var q market.Quote
	switch sym.AssetClass {
	case market.Future:
		quote, _, err := s.storeYahooChart(ctx, id, sym.ProviderSymbol, false)
		if err != nil {
			return err
		}
		q = quote
	case market.Stock:
		if market.YahooIndex(sym.ProviderSymbol) || yahooListed(sym.ProviderSymbol) {
			quote, _, err := s.storeYahooChart(ctx, id, sym.ProviderSymbol, yahooAdjust(sym.ProviderSymbol))
			if err != nil {
				return err
			}
			q = quote
			break
		}
		if s.Finnhub == nil || !s.Finnhub.Enabled() {
			return finnhub.ErrNoKey
		}
		quote, err := s.Finnhub.Quote(ctx, sym.ProviderSymbol)
		if err != nil {
			return err
		}
		q = quote
	case market.Crypto:
		got, err := s.Binance.Tickers(ctx, []string{sym.ProviderSymbol})
		if err != nil {
			return err
		}
		quote, ok := got[sym.ProviderSymbol]
		if !ok {
			return fmt.Errorf("no binance ticker for %s", sym.ProviderSymbol)
		}
		q = quote
	default:
		return fmt.Errorf("unknown asset class %s", sym.AssetClass)
	}
	s.attachIntraday(ctx, sym.AssetClass, sym.ProviderSymbol, &q)
	return s.writeQuote(ctx, sym.AssetClass, id, q)
}

// attachIntraday loads 5-minute closes. A failed fetch leaves IntradaySet false
// so the previous 1H and 4H stay on the quote row.
func (s *Syncer) attachIntraday(ctx context.Context, class, provider string, q *market.Quote) {
	var (
		points []market.Point
		ext    yahoo.Extended
		err    error
	)
	switch class {
	case market.Stock, market.Future:
		if s.Yahoo == nil {
			return
		}
		points, ext, err = s.Yahoo.Intraday(ctx, provider, class == market.Stock && !market.YahooIndex(provider))
	case market.Crypto:
		if s.Binance == nil {
			return
		}
		points, err = s.Binance.Intraday(ctx, provider)
	default:
		return
	}
	if err != nil {
		slog.Error("intraday", "symbol", provider, "err", err)
		return
	}
	q.Intraday = points
	q.IntradaySet = true
	if class == market.Stock && !market.YahooIndex(provider) {
		q.ExtSession = ext.Session
		q.ExtPct = ext.Pct
		q.ExtSet = true
	}
}

// SpanCandles returns the candles for one chart range.
// Intraday ranges read the local series and fetch only what is missing.
// Daily ranges are sliced by the caller from stored sessions.
func (s *Syncer) SpanCandles(ctx context.Context, id int64, spanName string) ([]market.Candle, error) {
	span, ok := market.LookupSpan(spanName)
	if !ok || !span.Intraday() {
		return nil, fmt.Errorf("unknown intraday range %s", spanName)
	}
	sym, err := s.Store.Symbol(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, err := s.loadIntraday(ctx, id, sym.AssetClass, sym.ProviderSymbol, span.Interval)
	if err != nil {
		return nil, err
	}
	daily, err := s.Store.Bars(ctx, id)
	if err != nil {
		return nil, err
	}
	return market.ShapeSpan(span, market.TrimIntraday(sym.AssetClass, spanName, raw, daily, s.now())), nil
}

// warmIntraday fills the 15-minute and hourly series once, in the background add job.
func (s *Syncer) warmIntraday(ctx context.Context, id int64) error {
	sym, err := s.Store.Symbol(ctx, id)
	if err != nil {
		return err
	}
	var first error
	for _, interval := range []string{market.Interval15m, market.Interval60m} {
		if _, err := s.loadIntraday(ctx, id, sym.AssetClass, sym.ProviderSymbol, interval); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Syncer) loadIntraday(ctx context.Context, id int64, class, provider, interval string) ([]market.IntradayBar, error) {
	key := strconv.FormatInt(id, 10) + ":" + interval
	s.mu.Lock()
	if wait, ok := s.spans[key]; ok {
		s.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return s.Store.IntradayBars(ctx, id, interval, market.CacheStart(interval, s.now()))
	}
	ch := make(chan struct{})
	s.spans[key] = ch
	s.mu.Unlock()

	err := s.fillIntraday(ctx, id, class, provider, interval)

	s.mu.Lock()
	delete(s.spans, key)
	close(ch)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.Store.IntradayBars(ctx, id, interval, market.CacheStart(interval, s.now()))
}

func (s *Syncer) fillIntraday(ctx context.Context, id int64, class, provider, interval string) error {
	now := s.now()
	start := market.CacheStart(interval, now)
	have, err := s.Store.IntradayBars(ctx, id, interval, start)
	if err != nil {
		return err
	}
	switch market.IntradayFetch(have, start, now, interval) {
	case market.FetchNone:
		return nil
	case market.FetchTail:
		got, err := s.fetchIntradaySince(ctx, class, provider, interval, time.Unix(have[len(have)-1].OpenUnix, 0))
		if err != nil {
			return nil
		}
		if err := s.Store.UpsertIntraday(ctx, id, interval, got); err != nil {
			return err
		}
	default:
		got, err := s.fetchIntradayFull(ctx, class, provider, interval, time.Unix(start, 0))
		if err != nil {
			return err
		}
		if err := s.Store.UpsertIntraday(ctx, id, interval, got); err != nil {
			return err
		}
	}
	return s.Store.PruneIntraday(ctx, id, interval, start)
}

func (s *Syncer) fetchIntradayFull(ctx context.Context, class, provider, interval string, since time.Time) ([]market.IntradayBar, error) {
	switch class {
	case market.Stock, market.Future:
		if s.Yahoo == nil {
			return nil, fmt.Errorf("yahoo unavailable")
		}
		return s.Yahoo.Candles(ctx, provider, interval, market.YahooRange(interval))
	case market.Crypto:
		if s.Binance == nil {
			return nil, fmt.Errorf("binance unavailable")
		}
		return s.Binance.Candles(ctx, provider, binanceInterval(interval), since)
	default:
		return nil, fmt.Errorf("unknown asset class %s", class)
	}
}

func (s *Syncer) fetchIntradaySince(ctx context.Context, class, provider, interval string, from time.Time) ([]market.IntradayBar, error) {
	switch class {
	case market.Stock, market.Future:
		if s.Yahoo == nil {
			return nil, fmt.Errorf("yahoo unavailable")
		}
		return s.Yahoo.CandlesSince(ctx, provider, interval, from, s.now())
	case market.Crypto:
		if s.Binance == nil {
			return nil, fmt.Errorf("binance unavailable")
		}
		return s.Binance.Candles(ctx, provider, binanceInterval(interval), from)
	default:
		return nil, fmt.Errorf("unknown asset class %s", class)
	}
}

func binanceInterval(interval string) string {
	if interval == market.Interval60m {
		return "1h"
	}
	return interval
}

// Backfill loads about five years of daily bars. Concurrent callers share one fetch.
func (s *Syncer) Backfill(ctx context.Context, id int64) error {
	s.mu.Lock()
	if wait, ok := s.inflight[id]; ok {
		s.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		n, err := s.Store.BarCount(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("history unavailable")
		}
		return nil
	}
	ch := make(chan struct{})
	s.inflight[id] = ch
	s.mu.Unlock()

	err := s.fetchAndStore(ctx, id, true)

	s.mu.Lock()
	delete(s.inflight, id)
	close(ch)
	s.mu.Unlock()
	return err
}

func (s *Syncer) fetchAndStore(ctx context.Context, id int64, full bool) error {
	sym, err := s.Store.Symbol(ctx, id)
	if err != nil {
		return err
	}
	bars, err := s.fetchBars(ctx, sym.AssetClass, sym.ProviderSymbol, full)
	if err != nil {
		return err
	}
	if len(bars) == 0 {
		return fmt.Errorf("no bars for %s", sym.Display)
	}
	if err := s.Store.UpsertBars(ctx, id, bars); err != nil {
		return err
	}
	if _, err := s.RefreshRanges(ctx); err != nil {
		return err
	}
	_, err = s.RefreshSymbolAverages(ctx, id)
	return err
}

func (s *Syncer) fetchBars(ctx context.Context, class, provider string, full bool) ([]market.Bar, error) {
	switch class {
	case market.Crypto:
		return s.Binance.DailyBars(ctx, provider, full)
	case market.Future:
		return s.Yahoo.DailyBars(ctx, provider, false, full)
	case market.Stock:
		if market.YahooIndex(provider) || yahooListed(provider) {
			if s.Yahoo == nil {
				return nil, fmt.Errorf("yahoo unavailable")
			}
			return s.Yahoo.DailyBars(ctx, provider, yahooAdjust(provider), full)
		}
		if s.stockCandles(ctx) {
			return s.Finnhub.DailyBars(ctx, provider, full)
		}
		return s.Yahoo.DailyBars(ctx, provider, true, full)
	default:
		return nil, fmt.Errorf("unknown asset class %s", class)
	}
}

func yahooListed(provider string) bool {
	return strings.Contains(provider, ".") && !market.YahooIndex(provider)
}

// yahooAdjust is false for an index, so a yield stays the printed level.
// A dotted equity still scales OHLC by the adjusted close.
func yahooAdjust(provider string) bool {
	return !market.YahooIndex(provider)
}

func (s *Syncer) storeYahooChart(ctx context.Context, id int64, provider string, adjust bool) (market.Quote, bool, error) {
	if s.Yahoo == nil {
		return market.Quote{}, false, fmt.Errorf("yahoo unavailable")
	}
	q, bars, err := s.Yahoo.Chart(ctx, provider, "5d", adjust)
	if err != nil {
		return market.Quote{}, false, err
	}
	if len(bars) == 0 {
		return q, false, nil
	}
	if err := s.Store.UpsertBars(ctx, id, bars); err != nil {
		return market.Quote{}, false, fmt.Errorf("%w: %s", errStoreBars, err)
	}
	return q, true, nil
}

func (s *Syncer) pullYahoo(ctx context.Context, sym store.TickSymbol, adjust bool, quotes map[int64]market.Quote, barsTouched map[int64]bool) error {
	q, touched, err := s.storeYahooChart(ctx, sym.ID, sym.ProviderSymbol, adjust)
	if err != nil {
		if errors.Is(err, errStoreBars) {
			return err
		}
		slog.Error("yahoo quote", "symbol", sym.ProviderSymbol, "err", err)
		return nil
	}
	s.attachIntraday(ctx, sym.AssetClass, sym.ProviderSymbol, &q)
	quotes[sym.ID] = q
	if touched {
		barsTouched[sym.ID] = true
	}
	return nil
}

func (s *Syncer) stockCandles(ctx context.Context) bool {
	v, ok, err := s.Store.Meta(ctx, "finnhub_candles")
	if err == nil && ok {
		return v == "1"
	}
	if s.Finnhub == nil || !s.Finnhub.Enabled() {
		return false
	}
	allowed, err := s.Finnhub.ProbeCandles(ctx)
	if err != nil {
		slog.Warn("finnhub candle probe", "err", err)
		return false
	}
	val := "0"
	if allowed {
		val = "1"
	}
	_ = s.Store.SetMeta(ctx, "finnhub_candles", val)
	return allowed
}

// Tick is the Coolify scheduled task. Overlapping runs no-op.
func (s *Syncer) Tick(ctx context.Context) error {
	ok, err := s.Store.TryLock(ctx, "tick", 90*time.Second)
	if err != nil {
		return err
	}
	if !ok {
		slog.Info("tick skipped, lock held")
		return nil
	}
	defer func() {
		if err := s.Store.Unlock(context.Background(), "tick"); err != nil {
			slog.Error("unlock tick", "err", err)
		}
	}()

	missing, err := s.Store.SymbolsWithoutBars(ctx, 1)
	if err != nil {
		return err
	}
	for _, id := range missing {
		if err := s.Backfill(ctx, id); err != nil {
			slog.Error("backfill", "symbol", id, "err", err)
		}
	}

	symbols, err := s.Store.TickSymbols(ctx)
	if err != nil {
		return err
	}
	quotes, refreshedBars, err := s.refreshQuotes(ctx, symbols)
	if err != nil {
		return err
	}
	if err := s.persistQuotes(ctx, symbols, quotes); err != nil {
		return err
	}
	pushed, err := s.evalAlerts(ctx, quotes)
	if err != nil {
		return err
	}
	tails, err := s.refreshTails(ctx, symbols, refreshedBars, 3)
	if err != nil {
		return err
	}
	ranges, err := s.RefreshRanges(ctx)
	if err != nil {
		return err
	}
	averages, err := s.RefreshAverages(ctx)
	if err != nil {
		return err
	}
	facts, err := s.refreshStaleFact(ctx, symbols)
	if err != nil {
		if errors.Is(err, finnhub.ErrRateLimit) {
			slog.Warn("facts rate limit")
		} else {
			slog.Error("facts", "err", err)
		}
	}
	logos, err := s.refreshLogos(ctx)
	if err != nil {
		if errors.Is(err, finnhub.ErrRateLimit) {
			slog.Warn("logo rate limit")
		} else {
			slog.Error("logo", "err", err)
		}
	}
	slog.Info("tick", "quotes", len(quotes), "pushed", pushed, "tails", tails, "ranges", ranges, "averages", averages, "facts", facts, "logos", logos)
	return nil
}

// RefreshFacts loads the company snapshot for one stock. A failed call returns
// before writing, so the stored row stays put. Futures and crypto are skipped.
// The annual filing is the fourth call: a 10-Q income statement is year-to-date,
// and the fourth quarter is that year minus the prior year-to-date.
func (s *Syncer) RefreshFacts(ctx context.Context, id int64) error {
	sym, err := s.Store.Symbol(ctx, id)
	if err != nil {
		return err
	}
	if sym.AssetClass != market.Stock || yahooListed(sym.ProviderSymbol) || market.YahooIndex(sym.ProviderSymbol) || s.Finnhub == nil || !s.Finnhub.Enabled() {
		return nil
	}
	marketCap, pe, err := s.Finnhub.Metric(ctx, sym.ProviderSymbol)
	if err != nil {
		return err
	}
	date, hour, err := s.Finnhub.NextEarnings(ctx, sym.ProviderSymbol, s.now())
	if err != nil {
		return err
	}
	revenue, earnings, err := s.Finnhub.ReportedTTM(ctx, sym.ProviderSymbol, s.now())
	if err != nil {
		return err
	}
	return s.Store.UpsertFacts(ctx, id, marketCap, pe, revenue, earnings, date, hour, s.now().Unix())
}

// refreshStaleFact refreshes one stock whose snapshot is missing or older than 20 hours.
func (s *Syncer) refreshStaleFact(ctx context.Context, symbols []store.TickSymbol) (int, error) {
	if s.Finnhub == nil || !s.Finnhub.Enabled() {
		return 0, nil
	}
	ids := make([]int64, 0, len(symbols))
	for _, sym := range symbols {
		if sym.AssetClass == market.Stock && !yahooListed(sym.ProviderSymbol) && !market.YahooIndex(sym.ProviderSymbol) {
			ids = append(ids, sym.ID)
		}
	}
	id, ok, err := s.Store.StaleFact(ctx, ids, s.now().Add(-20*time.Hour).Unix())
	if err != nil || !ok {
		return 0, err
	}
	if err := s.RefreshFacts(ctx, id); err != nil {
		return 0, err
	}
	return 1, nil
}

// RefreshRanges rewrites a cached band when the prior close it is centered on
// is no longer basis_day. The session the blotter is scoring is left out, so
// a closed day stays measured against the close before it. A current band is
// left untouched. The count is how many bands were written.
func (s *Syncer) RefreshRanges(ctx context.Context) (int, error) {
	statuses, err := s.Store.RangeStatuses(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now()
	n := 0
	for _, st := range statuses {
		basis := market.BandBasis(st.AssetClass, st.LastDay, st.PrevDay, now)
		if basis == "" || basis == st.BasisDay {
			continue
		}
		bars, err := s.Store.Bars(ctx, st.SymbolID)
		if err != nil {
			return n, err
		}
		done := market.BandBars(st.AssetClass, bars, now)
		if len(done) == 0 || done[len(done)-1].Day != basis {
			continue
		}
		low, high, ok := market.ExpectedBand(done)
		if !ok {
			continue
		}
		if err := s.Store.UpsertRange(ctx, st.SymbolID, basis, low, high, now.Unix()); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// RefreshAverages rebuilds cached daily EMA and SMA lines whose settings or
// last daily close have changed. Unchanged symbols are left as stored.
func (s *Syncer) RefreshAverages(ctx context.Context) (int, error) {
	return s.refreshAverages(ctx, 0)
}

// RefreshSymbolAverages rebuilds the lines for one symbol after its daily bars change.
func (s *Syncer) RefreshSymbolAverages(ctx context.Context, id int64) (int, error) {
	return s.refreshAverages(ctx, id)
}

func (s *Syncer) refreshAverages(ctx context.Context, only int64) (int, error) {
	settings, err := s.Store.AverageSettings(ctx)
	if err != nil {
		return 0, err
	}
	work, err := s.Store.AverageWork(ctx, only)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sym := range work {
		if sym.LastDay == "" {
			continue
		}
		var bars []market.Bar
		loaded := false
		for _, line := range settings {
			st, ok := sym.State[line.Slot]
			if ok && st.Kind == line.Kind && st.Period == line.Period && st.LastDay == sym.LastDay && st.LastClose == sym.LastClose {
				continue
			}
			if !loaded {
				bars, err = s.Store.Bars(ctx, sym.SymbolID)
				if err != nil {
					return n, err
				}
				loaded = true
			}
			points := market.MovingAverage(line.Kind, line.Period, bars)
			if err := s.Store.ReplaceAverage(ctx, sym.SymbolID, line.Slot, line.Kind, line.Period, sym.LastDay, sym.LastClose, points); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func (s *Syncer) refreshQuotes(ctx context.Context, symbols []store.TickSymbol) (map[int64]market.Quote, map[int64]bool, error) {
	quotes := map[int64]market.Quote{}
	barsTouched := map[int64]bool{}
	var mu sync.Mutex

	var futures, cryptos, stocks, yahooStocks []store.TickSymbol
	for _, sym := range symbols {
		switch sym.AssetClass {
		case market.Future:
			futures = append(futures, sym)
		case market.Crypto:
			cryptos = append(cryptos, sym)
		case market.Stock:
			if market.YahooIndex(sym.ProviderSymbol) || yahooListed(sym.ProviderSymbol) {
				yahooStocks = append(yahooStocks, sym)
			} else {
				stocks = append(stocks, sym)
			}
		}
	}

	for _, sym := range futures {
		if err := s.pullYahoo(ctx, sym, false, quotes, barsTouched); err != nil {
			return nil, nil, err
		}
	}
	for _, sym := range yahooStocks {
		if err := s.pullYahoo(ctx, sym, yahooAdjust(sym.ProviderSymbol), quotes, barsTouched); err != nil {
			return nil, nil, err
		}
	}

	if len(cryptos) > 0 {
		providers := make([]string, len(cryptos))
		byProvider := map[string]int64{}
		for i, sym := range cryptos {
			providers[i] = sym.ProviderSymbol
			byProvider[sym.ProviderSymbol] = sym.ID
		}
		got, err := s.Binance.Tickers(ctx, providers)
		if err != nil {
			slog.Error("binance tickers", "err", err)
		} else {
			for provider, q := range got {
				if id, ok := byProvider[provider]; ok {
					s.attachIntraday(ctx, market.Crypto, provider, &q)
					quotes[id] = q
				}
			}
		}
	}

	if len(stocks) > 0 && s.Finnhub != nil && s.Finnhub.Enabled() {
		cursor := int64(0)
		if v, ok, err := s.Store.Meta(ctx, "finnhub_cursor"); err == nil && ok {
			cursor, _ = strconv.ParseInt(v, 10, 64)
		}
		chosen, next := SelectStocks(stocks, cursor, 50)
		if err := s.Store.SetMeta(ctx, "finnhub_cursor", strconv.FormatInt(next, 10)); err != nil {
			return nil, nil, err
		}
		if len(chosen) > 50 {
			slog.Warn("finnhub armed alerts exceed the 50 call cap", "count", len(chosen))
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)
		for _, sym := range chosen {
			wg.Add(1)
			go func(sym store.TickSymbol) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				q, err := s.Finnhub.Quote(ctx, sym.ProviderSymbol)
				if err != nil {
					slog.Error("finnhub quote", "symbol", sym.ProviderSymbol, "err", err)
					return
				}
				s.attachIntraday(ctx, market.Stock, sym.ProviderSymbol, &q)
				mu.Lock()
				quotes[sym.ID] = q
				mu.Unlock()
			}(sym)
		}
		wg.Wait()
	} else if len(stocks) > 0 {
		slog.Warn("skipping stock quotes, FINNHUB_API_KEY is not set")
	}
	return quotes, barsTouched, nil
}

func (s *Syncer) persistQuotes(ctx context.Context, symbols []store.TickSymbol, quotes map[int64]market.Quote) error {
	byID := map[int64]store.TickSymbol{}
	for _, sym := range symbols {
		byID[sym.ID] = sym
	}
	for id, q := range quotes {
		sym, ok := byID[id]
		if !ok {
			continue
		}
		if err := s.writeQuote(ctx, sym.AssetClass, id, q); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) writeQuote(ctx context.Context, class string, id int64, q market.Quote) error {
	bars, err := s.Store.Bars(ctx, id)
	if err != nil {
		return err
	}
	d, w, m := market.Performance(class, q, bars, s.now())
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = s.now()
	}
	var h1, h4 *float64
	var spark []float64
	if q.IntradaySet {
		h1 = market.LookbackPct(q.Price, q.Intraday, asOf, time.Hour)
		h4 = market.LookbackPct(q.Price, q.Intraday, asOf, 4*time.Hour)
		spark = market.SessionCloses(class, q.Intraday, asOf)
	}
	var prev *float64
	if q.PrevClose > 0 {
		v := q.PrevClose
		prev = &v
	}
	return s.Store.UpsertQuote(ctx, id, q.Price, prev, d, w, m, h1, h4, spark, asOf.Unix(), q.IntradaySet, q.ExtSession, q.ExtPct, q.ExtSet)
}

func (s *Syncer) evalAlerts(ctx context.Context, quotes map[int64]market.Quote) (int, error) {
	armed, err := s.Store.ArmedAlerts(ctx)
	if err != nil {
		return 0, err
	}
	pushed := 0
	now := s.now().Unix()
	for _, a := range armed {
		q, ok := quotes[a.SymbolID]
		if !ok {
			continue
		}
		fire, next := alerts.Evaluate(a.Direction, a.Level, a.LastPrice, q.Price)
		if !fire {
			if err := s.Store.SetAlertLastPrice(ctx, a.ID, next); err != nil {
				return pushed, err
			}
			continue
		}
		var fired bool
		if a.Repeat == "continuous" {
			fired, err = s.Store.NoteContinuousFire(ctx, a.ID, q.Price, now)
		} else {
			fired, err = s.Store.TriggerAlert(ctx, a.ID, q.Price, now)
		}
		if err != nil {
			return pushed, err
		}
		if !fired {
			continue
		}
		if a.Repeat != "continuous" {
			if err := s.Store.CancelBracketMate(ctx, a.ID); err != nil {
				return pushed, err
			}
		}
		title := fmt.Sprintf("%s crossed %s %s", a.Display, alerts.CrossPhrase(a.Direction), market.FormatNumber(a.Level))
		body := "Last " + market.FormatNumber(q.Price)
		if a.Name != "" && !strings.EqualFold(a.Name, a.Display) {
			body = a.Name + " · " + body
		}
		if s.Push != nil {
			s.Push.Notify(ctx, push.Note{Title: title, Body: body, SymbolID: a.SymbolID, AlertID: a.ID})
		}
		pushed++
	}
	return pushed, nil
}

func (s *Syncer) refreshTails(ctx context.Context, symbols []store.TickSymbol, skip map[int64]bool, limit int) (int, error) {
	now := s.now()
	var due []store.TickSymbol
	for _, sym := range symbols {
		if skip[sym.ID] || sym.BarCount == 0 {
			continue
		}
		var synced *time.Time
		if sym.BarsSyncedAt != nil {
			t := time.Unix(*sym.BarsSyncedAt, 0)
			synced = &t
		}
		if market.NeedsTail(sym.AssetClass, sym.LastBar, synced, now) {
			due = append(due, sym)
		}
	}
	// Oldest sync first. TickSymbol order is by id; stable-sort by synced time.
	for i := 1; i < len(due); i++ {
		j := i
		for j > 0 && syncedUnix(due[j]) < syncedUnix(due[j-1]) {
			due[j], due[j-1] = due[j-1], due[j]
			j--
		}
	}
	if len(due) > limit {
		due = due[:limit]
	}
	n := 0
	for _, sym := range due {
		bars, err := s.fetchBars(ctx, sym.AssetClass, sym.ProviderSymbol, false)
		if err != nil {
			slog.Error("tail", "symbol", sym.ProviderSymbol, "err", err)
			continue
		}
		if err := s.Store.UpsertBars(ctx, sym.ID, bars); err != nil {
			return n, err
		}
		if q, err := s.Store.Quote(ctx, sym.ID); err == nil {
			mq := market.Quote{Price: q.Price, AsOf: time.Unix(q.AsOf, 0)}
			if q.PrevClose != nil {
				mq.PrevClose = *q.PrevClose
			}
			mq.DayPct = q.ChangeD
			if err := s.writeQuote(ctx, sym.AssetClass, sym.ID, mq); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}

func syncedUnix(s store.TickSymbol) int64 {
	if s.BarsSyncedAt == nil {
		return 0
	}
	return *s.BarsSyncedAt
}
