package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"stockwatcher/internal/config"
	"stockwatcher/internal/market"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
)

func TestBarsRange(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Now().In(ny)
	open := anchor.Unix()
	open -= open % (15 * 60)
	yahooSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"chart":{"result":[{"timestamp":[` + strconv.FormatInt(open, 10) + `],"indicators":{"quote":[{"open":[9],"high":[11],"low":[8],"close":[10]}]}}],"error":null}}`
		_, _ = w.Write([]byte(body))
	}))
	defer yahooSrv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gid int64
	for _, g := range board.Groups {
		if !g.Synthetic {
			gid = g.ID
		}
	}
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{
		AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple",
	})
	if err != nil {
		t.Fatal(err)
	}
	prev := 100.0
	day := 1.5
	if err := st.UpsertQuote(ctx, sym.ID, 110, &prev, &day, nil, nil, nil, nil, nil, nil, nil, time.Now().Unix(), false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	var daily []market.Bar
	for i := 0; i < 80; i++ {
		day := anchor.AddDate(0, 0, -79+i).Format("2006-01-02")
		daily = append(daily, market.Bar{Day: day, Open: 1, High: 2, Low: 1, Close: float64(i + 1)})
	}
	if err := st.UpsertBars(ctx, sym.ID, daily); err != nil {
		t.Fatal(err)
	}

	srv := New(st, syncer.New(st, nil, &yahoo.Client{Base: yahooSrv.URL}, nil, nil), config.Config{AccessToken: "testtoken"})
	h := srv.Handler()
	get := func(q string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/bars?id="+strconv.FormatInt(sym.ID, 10)+q, nil)
		req.Header.Set("Authorization", "Bearer testtoken")
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("&range=D")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Range      string   `json:"range"`
		Change     *float64 `json:"change"`
		ChangeBase *float64 `json:"change_base"`
		Bars       []struct {
			Time  int64   `json:"time"`
			Close float64 `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Range != "D" || body.Change == nil || *body.Change != 1.5 || body.ChangeBase == nil || *body.ChangeBase != 100 {
		t.Fatalf("day %+v", body)
	}
	if len(body.Bars) != 1 || body.Bars[0].Time != open || body.Bars[0].Close != 10 {
		t.Fatalf("day bars %+v", body.Bars)
	}

	rec = get("&range=3M")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var quarter struct {
		Range      string   `json:"range"`
		ChangeBase *float64 `json:"change_base"`
		Bars       []struct {
			Close float64 `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &quarter); err != nil {
		t.Fatal(err)
	}
	_, wantBase := market.RangeMove(market.Stock, "3M", 110, daily, anchor)
	if quarter.Range != "3M" || quarter.ChangeBase == nil || *quarter.ChangeBase != wantBase || len(quarter.Bars) == 0 {
		t.Fatalf("3M %+v want base %v", quarter, wantBase)
	}

	rec = get("&range=6M")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var half struct {
		Candle string `json:"candle"`
		Bars   []struct {
			Close float64 `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &half); err != nil {
		t.Fatal(err)
	}
	if half.Candle != "D" || len(half.Bars) != len(daily) {
		t.Fatalf("6M daily %+v", half)
	}

	rec = get("&range=6M&w=320")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var folded struct {
		Candle string `json:"candle"`
		Bars   []struct {
			Close float64 `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &folded); err != nil {
		t.Fatal(err)
	}
	shaped, fold := market.ShapeDaily(market.Stock, "6M", daily, time.Now(), 320)
	if fold != "3S" || folded.Candle != "3S" || len(folded.Bars) != len(shaped) || len(folded.Bars) >= len(half.Bars) {
		t.Fatalf("6M fold %+v want %s x%d", folded, fold, len(shaped))
	}
	if folded.Bars[len(folded.Bars)-1].Close != shaped[len(shaped)-1].Close {
		t.Fatalf("6M close %v", folded.Bars[len(folded.Bars)-1].Close)
	}

	rec = get("&range=6M&w=-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}

	rec = get("&range=nope")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestAveragesOnBars(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gid int64
	for _, g := range board.Groups {
		if !g.Synthetic {
			gid = g.ID
		}
	}
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{
		AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var daily []market.Bar
	for i := 0; i < 80; i++ {
		day := now.AddDate(0, 0, -79+i).Format("2006-01-02")
		daily = append(daily, market.Bar{Day: day, Open: 1, High: 2, Low: 1, Close: float64(i + 1)})
	}
	if err := st.UpsertBars(ctx, sym.ID, daily); err != nil {
		t.Fatal(err)
	}
	sy := syncer.New(st, nil, &yahoo.Client{}, nil, nil)
	if _, err := sy.RefreshAverages(ctx); err != nil {
		t.Fatal(err)
	}
	srv := New(st, sy, config.Config{AccessToken: "testtoken"})
	h := srv.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer testtoken")
		h.ServeHTTP(rec, req)
		return rec
	}

	settings := get("/api/averages")
	if settings.Code != http.StatusOK {
		t.Fatalf("settings %d %s", settings.Code, settings.Body.String())
	}
	var lines struct {
		Lines []struct {
			Slot   int    `json:"slot"`
			Kind   string `json:"kind"`
			Period int    `json:"period"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(settings.Body.Bytes(), &lines); err != nil {
		t.Fatal(err)
	}
	if len(lines.Lines) != 3 || lines.Lines[0].Period != 21 || lines.Lines[0].Kind != "ema" || lines.Lines[2].Kind != "sma" {
		t.Fatalf("defaults %+v", lines.Lines)
	}

	rec := get("/api/bars?id=" + strconv.FormatInt(sym.ID, 10) + "&range=3M")
	if rec.Code != http.StatusOK {
		t.Fatalf("bars %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Averages []struct {
			Slot   int    `json:"slot"`
			Kind   string `json:"kind"`
			Period int    `json:"period"`
			Points []struct {
				Value float64 `json:"value"`
			} `json:"points"`
		} `json:"averages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Averages) != 3 || body.Averages[0].Slot != 1 || len(body.Averages[0].Points) == 0 || len(body.Averages[2].Points) != 0 {
		t.Fatalf("averages %+v", body.Averages)
	}
	first := body.Averages[0].Points[0].Value
	for _, p := range body.Averages[0].Points {
		if p.Value == 0 {
			t.Fatalf("empty point %+v", p)
		}
	}
	if body.Averages[0].Points[len(body.Averages[0].Points)-1].Value < first {
		t.Fatal("rising closes should lift the average")
	}
}
