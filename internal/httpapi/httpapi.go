package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"stockwatcher/internal/config"
	"stockwatcher/internal/market"
	"stockwatcher/internal/market/finnhub"
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
	mux.HandleFunc("GET /api/search", s.auth(s.search))
	mux.HandleFunc("GET /api/bars", s.auth(s.bars))
	mux.HandleFunc("GET /api/alerts", s.auth(s.listAlerts))
	mux.HandleFunc("POST /api/alerts", s.auth(s.createAlert))
	mux.HandleFunc("DELETE /api/alerts/{id}", s.auth(s.deleteAlert))
	mux.HandleFunc("POST /api/alerts/{id}/rearm", s.auth(s.rearmAlert))
	mux.HandleFunc("GET /api/push/key", s.auth(s.pushKey))
	mux.HandleFunc("POST /api/push/subscribe", s.auth(s.subscribe))
	mux.HandleFunc("DELETE /api/push/subscribe", s.auth(s.unsubscribe))
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
		hits, err = s.Sync.Finnhub.Search(r.Context(), q)
		if err != nil && !errors.Is(err, finnhub.ErrNoKey) {
			slog.Error("search", "err", err)
			hits = nil
		}
		hits = withManualStock(hits, q)
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
	case market.Crypto:
		hits, err = s.Sync.Binance.Search(r.Context(), q)
		if err != nil {
			slog.Error("search", "err", err)
			writeErr(w, http.StatusBadGateway, "crypto search failed")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "class must be stock, future, or crypto")
		return
	}
	if hits == nil {
		hits = []market.Hit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
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
	res := r.URL.Query().Get("resolution")
	if res == "" {
		res = "D"
	}
	if res != "5m" && res != "1H" && res != "4H" && res != "D" && res != "W" && res != "M" {
		writeErr(w, http.StatusBadRequest, "resolution must be 5m, 1H, 4H, D, W, or M")
		return
	}
	sym, err := s.Store.Symbol(r.Context(), id)
	if mapErr(w, err) {
		return
	}
	if res == "5m" || res == "1H" || res == "4H" {
		candles, err := s.Sync.Candles(r.Context(), sym.AssetClass, sym.ProviderSymbol, res)
		if err != nil {
			slog.Error("intraday chart", "symbol", sym.Display, "resolution", res, "err", err)
			writeErr(w, http.StatusBadGateway, "history unavailable")
			return
		}
		s.writeBars(w, r, sym, res, candles)
		return
	}
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
	s.writeBars(w, r, sym, res, market.Rollup(bars, res))
}

func (s *Server) writeBars(w http.ResponseWriter, r *http.Request, sym store.Symbol, res string, candles []market.Candle) {
	var price, changeD *float64
	if q, err := s.Store.Quote(r.Context(), sym.ID); err == nil {
		v := q.Price
		price = &v
		changeD = q.ChangeD
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"symbol": map[string]any{
			"id":          sym.ID,
			"display":     sym.Display,
			"name":        sym.Name,
			"asset_class": sym.AssetClass,
			"price":       price,
			"change_d":    changeD,
		},
		"resolution": res,
		"bars":       candles,
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
	switch color {
	case "#c6a15b", "#8eae8a", "#c56b52", "#e8e0cc", "#8aa0ae", "#a35d6a":
	default:
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
