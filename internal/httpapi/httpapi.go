package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"stockwatcher/internal/alerts"
	"stockwatcher/internal/config"
	"stockwatcher/internal/market"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/push"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
	"stockwatcher/web"
)

var tickerPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.-]{0,9}$`)

// Server is the authenticated JSON API plus the embedded blotter.
type Server struct {
	Store *store.Store
	Sync  *syncer.Syncer
	Token string
	VAPID string
}

func New(st *store.Store, sy *syncer.Syncer, cfg config.Config) *Server {
	return &Server{Store: st, Sync: sy, Token: cfg.AccessToken, VAPID: cfg.VAPIDPublic}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/board", s.auth(s.board))
	mux.HandleFunc("POST /api/groups", s.auth(s.createGroup))
	mux.HandleFunc("PATCH /api/groups/{id}", s.auth(s.renameGroup))
	mux.HandleFunc("DELETE /api/groups/{id}", s.auth(s.deleteGroup))
	mux.HandleFunc("PUT /api/groups/order", s.auth(s.reorderGroups))
	mux.HandleFunc("POST /api/groups/{id}/symbols", s.auth(s.addSymbol))
	mux.HandleFunc("DELETE /api/groups/{id}/symbols/{symbolId}", s.auth(s.removeSymbol))
	mux.HandleFunc("PUT /api/groups/{id}/symbols/order", s.auth(s.reorderSymbols))
	mux.HandleFunc("GET /api/symbols/{id}/logo", s.auth(s.logo))
	mux.HandleFunc("PUT /api/owned/{symbolId}", s.auth(s.ownSymbol))
	mux.HandleFunc("DELETE /api/owned/{symbolId}", s.auth(s.unownSymbol))
	mux.HandleFunc("GET /api/search", s.auth(s.search))
	mux.HandleFunc("GET /api/bars", s.auth(s.bars))
	mux.HandleFunc("GET /api/averages", s.auth(s.listAverages))
	mux.HandleFunc("PUT /api/averages", s.auth(s.putAverages))
	mux.HandleFunc("GET /api/alerts", s.auth(s.listAlerts))
	mux.HandleFunc("POST /api/alerts", s.auth(s.createAlert))
	mux.HandleFunc("POST /api/alerts/bracket", s.auth(s.createBracket))
	mux.HandleFunc("DELETE /api/alerts/{id}", s.auth(s.deleteAlert))
	mux.HandleFunc("POST /api/alerts/{id}/rearm", s.auth(s.rearmAlert))
	mux.HandleFunc("GET /api/push/key", s.auth(s.pushKey))
	mux.HandleFunc("POST /api/push/subscribe", s.auth(s.subscribe))
	mux.HandleFunc("DELETE /api/push/subscribe", s.auth(s.unsubscribe))
	mux.HandleFunc("POST /api/push/test", s.auth(s.testPush))
	mux.Handle("/", web.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorized(s.Token, r.Header.Get("Authorization")) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func authorized(token, header string) bool {
	if token == "" {
		return false
	}
	const prefix = "Bearer "
	if len(header) < len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	got := header[len(prefix):]
	if len(got) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	board, err := s.Store.Board(r.Context())
	if mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
		Icon  string `json:"icon"`
	}
	if !decode(w, r, &body) {
		return
	}
	name, err := cleanName(body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	color, icon, err := cleanAppearance(body.Color, body.Icon)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	g, err := s.Store.CreateGroup(r.Context(), name, color, icon)
	if mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) renameGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
		Icon  string `json:"icon"`
	}
	if !decode(w, r, &body) {
		return
	}
	name, err := cleanName(body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	color, icon, err := cleanAppearance(body.Color, body.Icon)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if mapErr(w, s.Store.RenameGroup(r.Context(), id, name, color, icon)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if mapErr(w, s.Store.DeleteGroup(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ownSymbol(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "symbolId")
	if !ok {
		return
	}
	if mapErr(w, s.Store.SetOwned(r.Context(), id, true)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unownSymbol(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "symbolId")
	if !ok {
		return
	}
	if mapErr(w, s.Store.SetOwned(r.Context(), id, false)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	logo, err := s.Store.Logo(r.Context(), id)
	if mapErr(w, err) {
		return
	}
	if logo.MediaType != "" {
		w.Header().Set("Content-Type", logo.MediaType)
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(logo.Body)
}

func (s *Server) reorderGroups(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	if mapErr(w, s.Store.ReorderGroups(r.Context(), body.IDs)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addSymbol(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		AssetClass     string `json:"asset_class"`
		Display        string `json:"display"`
		ProviderSymbol string `json:"provider_symbol"`
		Name           string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	in, err := cleanSymbol(body.AssetClass, body.Display, body.ProviderSymbol, body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sym, created, err := s.Store.AddToGroup(r.Context(), groupID, in)
	if mapErr(w, err) {
		return
	}
	n, err := s.Store.BarCount(r.Context(), sym.ID)
	if mapErr(w, err) {
		return
	}
	if created || n == 0 {
		s.Sync.Enqueue(sym.ID)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":              sym.ID,
		"asset_class":     sym.AssetClass,
		"display":         sym.Display,
		"provider_symbol": sym.ProviderSymbol,
		"name":            sym.Name,
	})
}

func (s *Server) removeSymbol(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	symbolID, ok := pathID(w, r, "symbolId")
	if !ok {
		return
	}
	if mapErr(w, s.Store.RemoveFromGroup(r.Context(), groupID, symbolID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderSymbols(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	if mapErr(w, s.Store.ReorderSymbols(r.Context(), groupID, body.IDs)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	class := r.URL.Query().Get("class")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var (
		hits []market.Hit
		err  error
	)
	switch class {
	case market.Stock:
		hits, err = s.stockSearch(r, q)
		if errors.Is(err, finnhub.ErrNoKey) && len(hits) == 0 {
			writeErr(w, http.StatusBadRequest, "FINNHUB_API_KEY is not set")
			return
		}
		if err != nil && !errors.Is(err, finnhub.ErrNoKey) && len(hits) == 0 {
			writeErr(w, http.StatusBadGateway, "stock search failed")
			return
		}
	case market.Future:
		hits = market.SearchFutures(q)
	case "index":
		hits = market.SearchIndexes(q)
	case market.Crypto:
		hits, err = s.Sync.Binance.Search(r.Context(), q)
		if err != nil {
			slog.Error("search", "err", err)
			writeErr(w, http.StatusBadGateway, "crypto search failed")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "class must be stock, future, crypto, or index")
		return
	}
	if hits == nil {
		hits = []market.Hit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// stockSearch asks Finnhub first. Yahoo runs only when that list is empty.
// The typed US ticker is offered only after Yahoo has answered with nothing.
// A failed Yahoo call must not invent "SAMSUNG / US stock", which looks like
// a listing and hides the miss.
func (s *Server) stockSearch(r *http.Request, q string) ([]market.Hit, error) {
	var (
		hits []market.Hit
		err  error
	)
	if s.Sync == nil || s.Sync.Finnhub == nil {
		err = finnhub.ErrNoKey
	} else {
		hits, err = s.Sync.Finnhub.Search(r.Context(), q)
		if err != nil && !errors.Is(err, finnhub.ErrNoKey) {
			slog.Error("search", "err", err)
			hits = nil
		}
	}
	yahooAnswered := s.Sync == nil || s.Sync.Yahoo == nil
	if len(hits) == 0 && s.Sync != nil && s.Sync.Yahoo != nil {
		yh, yerr := s.Sync.Yahoo.Search(r.Context(), q)
		if yerr != nil {
			slog.Error("search", "err", yerr)
			if err == nil {
				err = yerr
			}
		} else {
			yahooAnswered = true
			if len(yh) > 0 {
				hits = yh
			}
		}
	}
	if len(hits) == 0 && yahooAnswered {
		hits = withManualStock(nil, q)
	}
	return hits, err
}

func withManualStock(hits []market.Hit, q string) []market.Hit {
	q = strings.ToUpper(strings.TrimSpace(q))
	if q == "" || !tickerPattern.MatchString(q) {
		return hits
	}
	for _, h := range hits {
		if h.ProviderSymbol == q {
			return hits
		}
	}
	manual := market.Hit{AssetClass: market.Stock, Display: q, ProviderSymbol: q, Name: "US stock"}
	return append([]market.Hit{manual}, hits...)
}

func (s *Server) bars(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "D"
	}
	span, ok := market.LookupSpan(rng)
	if !ok {
		writeErr(w, http.StatusBadRequest, "range must be D, 3D, 1W, 1M, 3M, 6M, YTD, 1Y, or 5Y")
		return
	}
	sym, err := s.Store.Symbol(r.Context(), id)
	if mapErr(w, err) {
		return
	}
	var candles []market.Candle
	if span.Intraday() {
		candles, err = s.Sync.SpanCandles(r.Context(), sym.ID, rng)
		if err != nil {
			slog.Error("intraday chart", "symbol", sym.Display, "range", rng, "err", err)
			writeErr(w, http.StatusBadGateway, "history unavailable")
			return
		}
	} else {
		n, err := s.Store.BarCount(r.Context(), id)
		if mapErr(w, err) {
			return
		}
		if n == 0 {
			if err := s.Sync.Backfill(r.Context(), id); err != nil {
				slog.Error("chart backfill", "symbol", sym.Display, "err", err)
				writeErr(w, http.StatusBadGateway, "history unavailable")
				return
			}
		}
		bars, err := s.Store.Bars(r.Context(), id)
		if mapErr(w, err) {
			return
		}
		candles = market.DailyCandles(sym.AssetClass, rng, bars, time.Now())
	}
	averages, err := s.chartAverages(r.Context(), sym, candles)
	if mapErr(w, err) {
		return
	}
	s.writeBars(w, r, sym, rng, candles, averages)
}

func (s *Server) chartAverages(ctx context.Context, sym store.Symbol, candles []market.Candle) ([]map[string]any, error) {
	lines, err := s.Store.AverageSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		points, err := s.Store.AveragePoints(ctx, sym.ID, line.Slot)
		if err != nil {
			return nil, err
		}
		marks := market.AlignAverage(sym.AssetClass, candles, points)
		if marks == nil {
			marks = []market.AverageMark{}
		}
		out = append(out, map[string]any{
			"slot":   line.Slot,
			"kind":   line.Kind,
			"period": line.Period,
			"color":  line.Color,
			"points": marks,
		})
	}
	return out, nil
}

func (s *Server) listAverages(w http.ResponseWriter, r *http.Request) {
	lines, err := s.Store.AverageSettings(r.Context())
	if mapErr(w, err) {
		return
	}
	if lines == nil {
		lines = []store.AverageSetting{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

func (s *Server) putAverages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lines []store.AverageSetting `json:"lines"`
	}
	if !decode(w, r, &body) {
		return
	}
	lines, err := cleanAverages(body.Lines)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if mapErr(w, s.Store.SaveAverageSettings(r.Context(), lines)) {
		return
	}
	if _, err := s.Sync.RefreshAverages(r.Context()); mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

func cleanAverages(lines []store.AverageSetting) ([]store.AverageSetting, error) {
	if len(lines) != 3 {
		return nil, errors.New("three lines are required")
	}
	seen := map[int]bool{}
	out := make([]store.AverageSetting, 0, 3)
	for _, line := range lines {
		if line.Slot < 1 || line.Slot > 3 || seen[line.Slot] {
			return nil, errors.New("slots must be 1, 2, and 3")
		}
		seen[line.Slot] = true
		kind := strings.ToLower(strings.TrimSpace(line.Kind))
		if kind != "ema" && kind != "sma" {
			return nil, errors.New("kind must be ema or sma")
		}
		if line.Period < 2 || line.Period > 400 {
			return nil, errors.New("period must be from 2 to 400")
		}
		color := strings.ToLower(strings.TrimSpace(line.Color))
		if !knownColor(color) {
			return nil, errors.New("unknown color")
		}
		out = append(out, store.AverageSetting{Slot: line.Slot, Kind: kind, Period: line.Period, Color: color})
	}
	return out, nil
}

func (s *Server) writeBars(w http.ResponseWriter, r *http.Request, sym store.Symbol, rng string, candles []market.Candle, averages []map[string]any) {
	var price, changeD, extPct, change, base *float64
	var extSession string
	now := time.Now()
	daily, _ := s.Store.Bars(r.Context(), sym.ID)
	if q, err := s.Store.Quote(r.Context(), sym.ID); err == nil {
		v := q.Price
		price = &v
		changeD = q.ChangeD
		extSession = q.ExtSession
		extPct = q.ExtPct
		if rng == "D" {
			change = q.ChangeD
			base = q.PrevClose
		} else if q.Price > 0 {
			pct, b := market.RangeMove(sym.AssetClass, rng, q.Price, daily, now)
			change = pct
			if b > 0 {
				base = &b
			}
		}
	}
	if base == nil && rng != "D" {
		if b, ok := market.RangeBase(sym.AssetClass, rng, daily, now); ok && b > 0 {
			v := b
			base = &v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"symbol": map[string]any{
			"id":              sym.ID,
			"display":         sym.Display,
			"name":            sym.Name,
			"asset_class":     sym.AssetClass,
			"provider_symbol": sym.ProviderSymbol,
			"price":           price,
			"change_d":        changeD,
			"ext_session":     extSession,
			"ext_pct":         extPct,
		},
		"range":       rng,
		"change":      change,
		"change_base": base,
		"bars":        candles,
		"averages":    averages,
	})
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListAlerts(r.Context())
	if mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": list})
}

func (s *Server) createAlert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SymbolID  int64   `json:"symbol_id"`
		Direction string  `json:"direction"`
		Repeat    string  `json:"repeat"`
		Level     float64 `json:"level"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Direction != "above" && body.Direction != "below" {
		writeErr(w, http.StatusBadRequest, "direction must be above or below")
		return
	}
	if body.Repeat == "" {
		body.Repeat = "once"
	}
	if body.Repeat != "once" && body.Repeat != "continuous" {
		writeErr(w, http.StatusBadRequest, "repeat must be once or continuous")
		return
	}
	if body.Level <= 0 {
		writeErr(w, http.StatusBadRequest, "level must be positive")
		return
	}
	a, err := s.Store.CreateAlert(r.Context(), body.SymbolID, body.Direction, body.Repeat, body.Level)
	if mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) createBracket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SymbolID int64   `json:"symbol_id"`
		Percent  float64 `json:"percent"`
	}
	if !decode(w, r, &body) {
		return
	}
	pair, err := s.Store.CreateBracket(r.Context(), body.SymbolID, body.Percent)
	switch {
	case errors.Is(err, alerts.ErrBadPercent):
		writeErr(w, http.StatusBadRequest, "percent must be greater than 0 and less than 100")
		return
	case errors.Is(err, alerts.ErrBadPrice), errors.Is(err, store.ErrNoQuote):
		writeErr(w, http.StatusBadRequest, "no price for this stock yet")
		return
	case errors.Is(err, alerts.ErrTight):
		writeErr(w, http.StatusBadRequest, "percent is too small for this price")
		return
	}
	if mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"alerts": pair})
}

func (s *Server) deleteAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if mapErr(w, s.Store.DeleteAlert(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rearmAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	a, err := s.Store.RearmAlert(r.Context(), id)
	if mapErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"public_key": s.VAPID,
		"enabled":    s.VAPID != "",
	})
}

func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !strings.HasPrefix(body.Endpoint, "https://") || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		writeErr(w, http.StatusBadRequest, "invalid push subscription")
		return
	}
	if mapErr(w, s.Store.SaveSubscription(r.Context(), body.Endpoint, body.Keys.P256dh, body.Keys.Auth)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testPush(w http.ResponseWriter, r *http.Request) {
	var sender *push.Sender
	if s.Sync != nil {
		sender = s.Sync.Push
	}
	if sender == nil || !sender.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "Push is not configured")
		return
	}
	subs, err := s.Store.Subscriptions(r.Context())
	if mapErr(w, err) {
		return
	}
	if len(subs) == 0 {
		writeErr(w, http.StatusBadRequest, "No push subscriptions")
		return
	}
	sender.Notify(r.Context(), push.Note{Title: "Stockwatcher", Body: "Test alert"})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "sent",
		"subscriptions": len(subs),
	})
}

func (s *Server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	if mapErr(w, s.Store.DeleteSubscription(r.Context(), body.Endpoint)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func knownColor(color string) bool {
	switch color {
	case "#c6a15b", "#8eae8a", "#c56b52", "#e8e0cc", "#8aa0ae", "#a35d6a", "#d4895c", "#a3ae62", "#5eaea4", "#6e92d4", "#b196cc", "#d08a9c":
		return true
	default:
		return false
	}
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 40 {
		return "", errors.New("name must be 1-40 characters")
	}
	return name, nil
}

func cleanAppearance(color, icon string) (string, string, error) {
	color = strings.ToLower(strings.TrimSpace(color))
	icon = strings.ToLower(strings.TrimSpace(icon))
	if color == "" {
		color = "#c6a15b"
	}
	if icon == "" {
		icon = "mark"
	}
	if !knownColor(color) {
		return "", "", errors.New("unknown color")
	}
	switch icon {
	case "mark", "eye", "chart", "coin", "bolt", "layers", "crypto", "bank", "chip", "memory", "ai", "energy", "pulse", "cloud", "factory", "house", "shield":
	default:
		return "", "", errors.New("unknown icon")
	}
	return color, icon, nil
}

func cleanSymbol(class, display, provider, name string) (store.NewSymbol, error) {
	class = strings.TrimSpace(class)
	if class != market.Stock && class != market.Future && class != market.Crypto {
		return store.NewSymbol{}, errors.New("asset_class must be stock, future, or crypto")
	}
	display = strings.ToUpper(strings.TrimSpace(display))
	provider = strings.ToUpper(strings.TrimSpace(provider))
	name = strings.TrimSpace(name)
	if display == "" || len(display) > 16 {
		return store.NewSymbol{}, errors.New("display symbol is required")
	}
	if provider == "" || len(provider) > 32 {
		return store.NewSymbol{}, errors.New("provider symbol is required")
	}
	if len([]rune(name)) > 80 {
		return store.NewSymbol{}, errors.New("name is too long")
	}
	switch class {
	case market.Future:
		if !strings.HasSuffix(provider, "=F") {
			return store.NewSymbol{}, errors.New("futures use a Yahoo symbol such as ES=F")
		}
	case market.Crypto:
		if !strings.HasSuffix(provider, "USDT") {
			return store.NewSymbol{}, errors.New("crypto uses a Binance USDT symbol such as BTCUSDT")
		}
	}
	if name == "" {
		name = display
	}
	return store.NewSymbol{AssetClass: class, Display: display, ProviderSymbol: provider, Name: name}, nil
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func mapErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrLastGroup):
		writeErr(w, http.StatusBadRequest, "keep at least one group")
	case errors.Is(err, store.ErrBadReorder):
		writeErr(w, http.StatusBadRequest, "invalid order")
	default:
		slog.Error("api", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
	return true
}
