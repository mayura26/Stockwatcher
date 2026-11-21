package syncer

import (
	"context"
	"fmt"
	"log/slog"
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

// Syncer refreshes quotes and bars, then evaluates price crosses.
type Syncer struct {
	Store   *store.Store
	Finnhub *finnhub.Client
	Yahoo   *yahoo.Client
	Binance *binance.Client
	Push    *push.Sender
	Now     func() time.Time

	mu       sync.Mutex
	inflight map[int64]chan struct{}
}

func New(st *store.Store, fh *finnhub.Client, yh *yahoo.Client, bn *binance.Client, ps *push.Sender) *Syncer {
	return &Syncer{
		Store:    st,
		Finnhub:  fh,
		Yahoo:    yh,
		Binance:  bn,
		Push:     ps,
		inflight: map[int64]chan struct{}{},
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
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := s.Backfill(ctx, id); err != nil {
			slog.Error("backfill", "symbol", id, "err", err)
		}
		if err := s.RefreshQuote(ctx, id); err != nil {
			slog.Error("quote", "symbol", id, "err", err)
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
		quote, bars, err := s.Yahoo.Chart(ctx, sym.ProviderSymbol, "5d", false)
		if err != nil {
			return err
		}
		q = quote
		if len(bars) > 0 {
			if err := s.Store.UpsertBars(ctx, id, bars); err != nil {
				return err
			}
		}
	case market.Stock:
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
		err    error
	)
	switch class {
	case market.Stock, market.Future:
		if s.Yahoo == nil {
			return
		}
		points, err = s.Yahoo.Intraday(ctx, provider)
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
	return s.Store.UpsertBars(ctx, id, bars)
}

func (s *Syncer) fetchBars(ctx context.Context, class, provider string, full bool) ([]market.Bar, error) {
	switch class {
	case market.Crypto:
		return s.Binance.DailyBars(ctx, provider, full)
	case market.Future:
		return s.Yahoo.DailyBars(ctx, provider, false, full)
	case market.Stock:
		if s.stockCandles(ctx) {
			return s.Finnhub.DailyBars(ctx, provider, full)
		}
		return s.Yahoo.DailyBars(ctx, provider, true, full)
	default:
		return nil, fmt.Errorf("unknown asset class %s", class)
	}
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
	slog.Info("tick", "quotes", len(quotes), "pushed", pushed, "tails", tails)
	return nil
}

func (s *Syncer) refreshQuotes(ctx context.Context, symbols []store.TickSymbol) (map[int64]market.Quote, map[int64]bool, error) {
	quotes := map[int64]market.Quote{}
	barsTouched := map[int64]bool{}
	var mu sync.Mutex

	var futures, cryptos, stocks []store.TickSymbol
	for _, sym := range symbols {
		switch sym.AssetClass {
		case market.Future:
			futures = append(futures, sym)
		case market.Crypto:
			cryptos = append(cryptos, sym)
		case market.Stock:
			stocks = append(stocks, sym)
		}
	}

	for _, sym := range futures {
		q, bars, err := s.Yahoo.Chart(ctx, sym.ProviderSymbol, "5d", false)
		if err != nil {
			slog.Error("yahoo quote", "symbol", sym.ProviderSymbol, "err", err)
			continue
		}
		s.attachIntraday(ctx, sym.AssetClass, sym.ProviderSymbol, &q)
		mu.Lock()
		quotes[sym.ID] = q
		mu.Unlock()
		if len(bars) > 0 {
			if err := s.Store.UpsertBars(ctx, sym.ID, bars); err != nil {
				return nil, nil, err
			}
			barsTouched[sym.ID] = true
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
	if q.IntradaySet {
		h1 = market.LookbackPct(q.Price, q.Intraday, asOf, time.Hour)
		h4 = market.LookbackPct(q.Price, q.Intraday, asOf, 4*time.Hour)
	}
	var prev *float64
	if q.PrevClose > 0 {
		v := q.PrevClose
		prev = &v
	}
	return s.Store.UpsertQuote(ctx, id, q.Price, prev, d, w, m, h1, h4, asOf.Unix(), q.IntradaySet)
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
