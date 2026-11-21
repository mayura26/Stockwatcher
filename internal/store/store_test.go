package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"stockwatcher/internal/alerts"
	"stockwatcher/internal/market"
)

func TestBoardGroupsAndLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "has space")
	st, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Groups) != 3 || board.Groups[0].Name != "Notable" || board.Groups[0].Kind != "notable" || !board.Groups[0].Synthetic || len(board.Groups[0].Rows) != 0 {
		t.Fatalf("%+v", board.Groups)
	}
	if board.Groups[1].Name != "Owned" || board.Groups[1].Kind != "owned" || board.Groups[1].ID != OwnedGroupID || !board.Groups[1].Synthetic || len(board.Groups[1].Rows) != 0 {
		t.Fatalf("%+v", board.Groups[1])
	}
	if board.Groups[2].Name != "Watch" || board.Groups[2].Color != "#c6a15b" || board.Groups[2].Icon != "mark" {
		t.Fatalf("%+v", board.Groups[2])
	}
	gid := board.Groups[2].ID
	sym, created, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Future, Display: "ES", ProviderSymbol: "ES=F", Name: "E-mini S&P 500"})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	day := 1.25
	h1 := 0.5
	if err := st.UpsertQuote(ctx, sym.ID, 5800, nil, &day, nil, nil, &h1, nil, []float64{1, 2, 3}, time.Now().Unix(), true, "", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertBars(ctx, sym.ID, []market.Bar{{Day: "2024-01-02", Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 3}}); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	watch := groupNamed(t, board, "Watch")
	row := watch.Rows[0]
	if len(watch.Rows) != 1 || row.Display != "ES" || row.Price == nil || *row.Price != 5800 || row.ChangeH1 == nil || *row.ChangeH1 != 0.5 || row.ChangeH4 != nil || len(row.Sparkline) != 3 || row.Sparkline[2] != 3 {
		t.Fatalf("%+v", watch.Rows)
	}
	if err := st.UpsertQuote(ctx, sym.ID, 5801, nil, &day, nil, nil, nil, nil, nil, time.Now().Unix(), false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row = groupNamed(t, board, "Watch").Rows[0]
	if row.Price == nil || *row.Price != 5801 || row.ChangeH1 == nil || *row.ChangeH1 != 0.5 || len(row.Sparkline) != 3 {
		t.Fatalf("preserved %+v", row)
	}
	ok, err := st.TryLock(ctx, "tick", time.Minute)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	ok, err = st.TryLock(ctx, "tick", time.Minute)
	if err != nil || ok {
		t.Fatal("second lock", err, ok)
	}
	if err := st.Unlock(ctx, "tick"); err != nil {
		t.Fatal(err)
	}
	ok, err = st.TryLock(ctx, "tick", time.Minute)
	if err != nil || !ok {
		t.Fatal("relock", err, ok)
	}
	if err := st.DeleteGroup(ctx, gid); err != ErrLastGroup {
		t.Fatal(err)
	}
	made, err := st.CreateGroup(ctx, "Metals", "#c56b52", "coin")
	if err != nil {
		t.Fatal(err)
	}
	if made.Color != "#c56b52" || made.Icon != "coin" {
		t.Fatalf("%+v", made)
	}
	if err := st.RenameGroup(ctx, made.ID, "Copper", "#a35d6a", "bolt"); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var copper Group
	for _, g := range board.Groups {
		if g.ID == made.ID {
			copper = g
		}
	}
	if copper.Name != "Copper" || copper.Color != "#a35d6a" || copper.Icon != "bolt" {
		t.Fatalf("%+v", copper)
	}
}

func TestGroupAppearanceMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE groups (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  position INTEGER NOT NULL
);
INSERT INTO groups(name, position) VALUES ('Legacy', 0);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	board, err := st.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Groups) != 3 || board.Groups[0].Name != "Notable" || !board.Groups[0].Synthetic || board.Groups[1].Name != "Owned" {
		t.Fatalf("%+v", board.Groups)
	}
	legacy := board.Groups[2]
	if legacy.Name != "Legacy" || legacy.Color != "#c6a15b" || legacy.Icon != "mark" {
		t.Fatalf("%+v", legacy)
	}
}

func TestNotableGroup(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid := groupNamed(t, board, "Watch").ID
	add := func(display string) Symbol {
		t.Helper()
		sym, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: display, ProviderSymbol: display, Name: display})
		if err != nil {
			t.Fatal(err)
		}
		return sym
	}
	quote := func(id int64, price float64, spark []float64) {
		t.Helper()
		if err := st.UpsertQuote(ctx, id, price, nil, nil, nil, nil, nil, nil, spark, 1, true, "", nil, false); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertRange(ctx, id, "2024-01-02", 10, 12, 1); err != nil {
			t.Fatal(err)
		}
	}
	inside := add("IN")
	spike := add("SP")
	far := add("FAR")
	quote(inside.ID, 11, []float64{11, 11.2})
	quote(spike.ID, 11, []float64{11, 13})
	quote(far.ID, 16, []float64{11, 11.4})
	alertOnly := add("AL")
	if err := st.RemoveFromGroup(ctx, gid, alertOnly.ID); err != nil {
		t.Fatal(err)
	}
	quote(alertOnly.ID, 9, []float64{11})
	if _, err := st.CreateAlert(ctx, alertOnly.ID, "below", "once", 8); err != nil {
		t.Fatal(err)
	}

	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	notable := board.Groups[0]
	if notable.Name != "Notable" || !notable.Synthetic || notable.ID != 0 {
		t.Fatalf("%+v", notable)
	}
	got := make([]string, len(notable.Rows))
	for i, row := range notable.Rows {
		got[i] = row.Display
	}
	// FAR is furthest past the band. SP and AL tie; SP was seen first.
	want := []string{"FAR", "SP", "AL"}
	if len(got) != len(want) {
		t.Fatalf("notable %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("notable %v", got)
		}
	}
	watch := groupNamed(t, board, "Watch")
	if len(watch.Rows) != 3 {
		t.Fatalf("watch %+v", watch.Rows)
	}
	var stamped Row
	var found bool
	for _, row := range watch.Rows {
		if row.Display == "SP" {
			stamped = row
			found = true
		}
	}
	if !found || stamped.RangeLow == nil || *stamped.RangeLow != 10 || stamped.RangeHigh == nil || *stamped.RangeHigh != 12 {
		t.Fatalf("stamped %+v", stamped)
	}
}

func TestOwnedGroup(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid := groupNamed(t, board, "Watch").ID
	add := func(display string) Symbol {
		t.Helper()
		sym, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: display, ProviderSymbol: display, Name: display})
		if err != nil {
			t.Fatal(err)
		}
		return sym
	}
	first := add("AAA")
	second := add("BBB")
	if err := st.SetOwned(ctx, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOwned(ctx, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOwned(ctx, second.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOwned(ctx, 99999, true); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := st.UpsertQuote(ctx, second.ID, 16, nil, nil, nil, nil, nil, nil, []float64{11}, 1, true, "", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRange(ctx, second.ID, "2024-01-02", 10, 12, 1); err != nil {
		t.Fatal(err)
	}

	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owned := groupNamed(t, board, "Owned")
	if len(owned.Rows) != 2 || owned.Rows[0].Display != "AAA" || owned.Rows[1].Display != "BBB" || !owned.Rows[0].Owned {
		t.Fatalf("%+v", owned.Rows)
	}
	watch := groupNamed(t, board, "Watch")
	var flagged bool
	for _, row := range watch.Rows {
		if row.Display == "AAA" && row.Owned {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("watch %+v", watch.Rows)
	}

	if err := st.SetOwned(ctx, first.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOwned(ctx, first.ID, false); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := st.RemoveFromGroup(ctx, gid, second.ID); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owned = groupNamed(t, board, "Owned")
	if len(owned.Rows) != 1 || owned.Rows[0].Display != "BBB" {
		t.Fatalf("%+v", owned.Rows)
	}
	notable := groupNamed(t, board, "Notable")
	var seen bool
	for _, row := range notable.Rows {
		if row.Display == "BBB" && row.Owned {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("notable %+v", notable.Rows)
	}
	ticks, err := st.TickSymbols(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ticking bool
	for _, sym := range ticks {
		if sym.ID == second.ID {
			ticking = true
		}
	}
	if !ticking {
		t.Fatalf("ticks %+v", ticks)
	}
}

func groupNamed(t *testing.T, board Board, name string) Group {
	t.Helper()
	for _, g := range board.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("missing %s", name)
	return Group{}
}

func TestAlertRepeatMigrationAndFire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE symbols (
  id INTEGER PRIMARY KEY,
  asset_class TEXT NOT NULL CHECK (asset_class IN ('stock','future','crypto')),
  display TEXT NOT NULL,
  provider_symbol TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  bars_synced_at INTEGER,
  UNIQUE (asset_class, provider_symbol)
);
CREATE TABLE alerts (
  id INTEGER PRIMARY KEY,
  symbol_id INTEGER NOT NULL,
  direction TEXT NOT NULL,
  level REAL NOT NULL,
  status TEXT NOT NULL DEFAULT 'armed',
  last_price REAL,
  triggered_at INTEGER,
  created_at INTEGER NOT NULL
);
INSERT INTO symbols(asset_class, display, provider_symbol, name) VALUES ('future', 'ES', 'ES=F', 'E-mini');
INSERT INTO alerts(symbol_id, direction, level, status, last_price, created_at) VALUES (1, 'above', 10, 'armed', 9, 1);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	legacy, err := st.Alert(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Repeat != "once" || legacy.Status != "armed" || legacy.BracketID != nil {
		t.Fatalf("migrated %+v", legacy)
	}

	once, err := st.CreateAlert(ctx, 1, "below", "once", 9)
	if err != nil {
		t.Fatal(err)
	}
	cont, err := st.CreateAlert(ctx, 1, "above", "continuous", 12)
	if err != nil {
		t.Fatal(err)
	}
	if once.Repeat != "once" || cont.Repeat != "continuous" {
		t.Fatalf("once %s continuous %s", once.Repeat, cont.Repeat)
	}
	ok, err := st.NoteContinuousFire(ctx, cont.ID, 12.5, 99)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	got, err := st.Alert(ctx, cont.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "armed" || got.LastPrice == nil || *got.LastPrice != 12.5 || got.TriggeredAt == nil || *got.TriggeredAt != 99 {
		t.Fatalf("%+v", got)
	}
	ok, err = st.NoteContinuousFire(ctx, once.ID, 8, 100)
	if err != nil || ok {
		t.Fatal("once alert should not take a continuous fire", err, ok)
	}
}

func TestBracketCancelsMate(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid := groupNamed(t, board, "Watch").ID
	sym, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertQuote(ctx, sym.ID, 100, nil, nil, nil, nil, nil, nil, nil, time.Now().Unix(), false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	pair, err := st.CreateBracket(ctx, sym.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair) != 2 || pair[0].Direction != "above" || pair[0].Level != 110 || pair[1].Direction != "below" || pair[1].Level != 90 {
		t.Fatalf("%+v", pair)
	}
	if pair[0].Repeat != "once" || pair[1].Repeat != "once" || pair[0].Status != "armed" || pair[1].Status != "armed" {
		t.Fatalf("legs %+v %+v", pair[0], pair[1])
	}
	if pair[0].BracketID == nil || pair[1].BracketID == nil || *pair[0].BracketID != pair[0].ID || *pair[1].BracketID != pair[0].ID {
		t.Fatalf("link %+v %+v", pair[0].BracketID, pair[1].BracketID)
	}
	ok, err := st.TriggerAlert(ctx, pair[0].ID, 111, 50)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if err := st.CancelBracketMate(ctx, pair[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Alert(ctx, pair[1].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mate %v", err)
	}
	fired, err := st.Alert(ctx, pair[0].ID)
	if err != nil || fired.Status != "triggered" {
		t.Fatalf("%+v %v", fired, err)
	}

	pair, err = st.CreateBracket(ctx, sym.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAlert(ctx, pair[0].ID); err != nil {
		t.Fatal(err)
	}
	kept, err := st.Alert(ctx, pair[1].ID)
	if err != nil || kept.BracketID != nil || kept.Status != "armed" || kept.Direction != "below" {
		t.Fatalf("%+v %v", kept, err)
	}

	other, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "MSFT", ProviderSymbol: "MSFT", Name: "Microsoft"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBracket(ctx, other.ID, 10); !errors.Is(err, ErrNoQuote) {
		t.Fatalf("quote %v", err)
	}
	if _, err := st.CreateBracket(ctx, sym.ID, 0); !errors.Is(err, alerts.ErrBadPercent) {
		t.Fatalf("percent %v", err)
	}
}

func TestQuoteHourlyMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE symbols (
  id INTEGER PRIMARY KEY,
  asset_class TEXT NOT NULL,
  display TEXT NOT NULL,
  provider_symbol TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT ''
);
CREATE TABLE quotes (
  symbol_id INTEGER PRIMARY KEY,
  price REAL NOT NULL,
  prev_close REAL,
  change_d REAL,
  change_w REAL,
  change_m REAL,
  as_of INTEGER NOT NULL
);
INSERT INTO symbols(asset_class, display, provider_symbol, name) VALUES ('stock', 'AAPL', 'AAPL', 'Apple');
INSERT INTO quotes(symbol_id, price, change_d, as_of) VALUES (1, 10, 1, 1);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	h1, h4 := 1.5, -2.0
	if err := st.UpsertQuote(ctx, 1, 11, nil, nil, nil, nil, &h1, &h4, nil, 2, true, "", nil, false); err != nil {
		t.Fatal(err)
	}
	q, err := st.Quote(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 11 || q.ChangeH1 == nil || *q.ChangeH1 != 1.5 || q.ChangeH4 == nil || *q.ChangeH4 != -2 {
		t.Fatalf("%+v", q)
	}
	if err := st.UpsertQuote(ctx, 1, 12, nil, nil, nil, nil, nil, nil, nil, 3, false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	q, err = st.Quote(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 12 || q.ChangeH1 == nil || *q.ChangeH1 != 1.5 || q.ChangeH4 == nil || *q.ChangeH4 != -2 {
		t.Fatalf("preserved %+v", q)
	}
}

func TestQuoteExtended(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE symbols (
  id INTEGER PRIMARY KEY,
  asset_class TEXT NOT NULL,
  display TEXT NOT NULL,
  provider_symbol TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT ''
);
CREATE TABLE quotes (
  symbol_id INTEGER PRIMARY KEY,
  price REAL NOT NULL,
  prev_close REAL,
  change_d REAL,
  change_w REAL,
  change_m REAL,
  as_of INTEGER NOT NULL
);
INSERT INTO symbols(asset_class, display, provider_symbol, name) VALUES ('stock', 'AAPL', 'AAPL', 'Apple');
INSERT INTO quotes(symbol_id, price, change_d, as_of) VALUES (1, 10, 1, 1);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	pre := 0.4
	if err := st.UpsertQuote(ctx, 1, 11, nil, nil, nil, nil, nil, nil, nil, 2, false, "pre", &pre, true); err != nil {
		t.Fatal(err)
	}
	q, err := st.Quote(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 11 || q.ExtSession != "pre" || q.ExtPct == nil || *q.ExtPct != 0.4 {
		t.Fatalf("%+v", q)
	}
	if err := st.UpsertQuote(ctx, 1, 12, nil, nil, nil, nil, nil, nil, nil, 3, false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	q, err = st.Quote(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 12 || q.ExtSession != "pre" || q.ExtPct == nil || *q.ExtPct != 0.4 {
		t.Fatalf("preserved %+v", q)
	}
	flat := 0.0
	if err := st.UpsertQuote(ctx, 1, 12, nil, nil, nil, nil, nil, nil, nil, 4, false, "post", &flat, true); err != nil {
		t.Fatal(err)
	}
	q, err = st.Quote(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.ExtSession != "post" || q.ExtPct == nil || *q.ExtPct != 0 {
		t.Fatalf("flat %+v", q)
	}
	if err := st.UpsertQuote(ctx, 1, 13, nil, nil, nil, nil, nil, nil, nil, 5, false, "", nil, true); err != nil {
		t.Fatal(err)
	}
	q, err = st.Quote(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 13 || q.ExtSession != "" || q.ExtPct != nil {
		t.Fatalf("cleared %+v", q)
	}

	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	board, err := fresh.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid := groupNamed(t, board, "Watch").ID
	sym, _, err := fresh.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	after := -1.25
	if err := fresh.UpsertQuote(ctx, sym.ID, 190, nil, nil, nil, nil, nil, nil, nil, 6, false, "post", &after, true); err != nil {
		t.Fatal(err)
	}
	board, err = fresh.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row := groupNamed(t, board, "Watch").Rows[0]
	if row.ExtSession != "post" || row.ExtPct == nil || *row.ExtPct != -1.25 {
		t.Fatalf("board %+v", row)
	}
	if err := fresh.RemoveFromGroup(ctx, gid, sym.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.CreateAlert(ctx, sym.ID, "above", "once", 200); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Board(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFactsMigrationAndBoard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE symbols (
  id INTEGER PRIMARY KEY,
  asset_class TEXT NOT NULL,
  display TEXT NOT NULL,
  provider_symbol TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  bars_synced_at INTEGER
);
CREATE TABLE groups (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  position INTEGER NOT NULL
);
CREATE TABLE group_symbols (
  group_id INTEGER NOT NULL,
  symbol_id INTEGER NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (group_id, symbol_id)
);
CREATE TABLE quotes (
  symbol_id INTEGER PRIMARY KEY,
  price REAL NOT NULL,
  prev_close REAL,
  change_d REAL,
  change_w REAL,
  change_m REAL,
  as_of INTEGER NOT NULL
);
INSERT INTO symbols(id, asset_class, display, provider_symbol, name) VALUES (1, 'stock', 'AAPL', 'AAPL', 'Apple');
INSERT INTO groups(id, name, position) VALUES (1, 'Watch', 0);
INSERT INTO group_symbols(group_id, symbol_id, position) VALUES (1, 1, 0);
INSERT INTO quotes(symbol_id, price, as_of) VALUES (1, 190, 1);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	cap, pe, rev, earn := 3.4e12, 32.4, 391e9, 96.2e9
	if err := st.UpsertFacts(ctx, 1, &cap, &pe, &rev, &earn, "2026-01-29", "amc", 100); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOwned(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	board, err := st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	watch := groupNamed(t, board, "Watch").Rows[0]
	owned := groupNamed(t, board, "Owned").Rows[0]
	for _, row := range []Row{watch, owned} {
		if row.MarketCap == nil || *row.MarketCap != cap || row.PeTTM == nil || *row.PeTTM != pe || row.RevenueTTM == nil || *row.RevenueTTM != rev || row.EarningsTTM == nil || *row.EarningsTTM != earn || row.EarningsDate != "2026-01-29" || row.EarningsHour != "amc" {
			t.Fatalf("%+v", row)
		}
	}

	gid := groupNamed(t, board, "Watch").ID
	msft, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "MSFT", ProviderSymbol: "MSFT", Name: "Microsoft"})
	if err != nil {
		t.Fatal(err)
	}
	es, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Future, Display: "ES", ProviderSymbol: "ES=F", Name: "E-mini"})
	if err != nil {
		t.Fatal(err)
	}
	id, ok, err := st.StaleFact(ctx, []int64{1, msft.ID, es.ID}, 50)
	if err != nil || !ok || id != msft.ID {
		t.Fatalf("missing facts id %d ok %v err %v", id, ok, err)
	}
	old := int64(10)
	if err := st.UpsertFacts(ctx, msft.ID, nil, nil, nil, nil, "", "", old); err != nil {
		t.Fatal(err)
	}
	id, ok, err = st.StaleFact(ctx, []int64{1, msft.ID, es.ID}, 50)
	if err != nil || !ok || id != msft.ID {
		t.Fatalf("older facts id %d ok %v err %v", id, ok, err)
	}
	if err := st.UpsertFacts(ctx, msft.ID, nil, nil, nil, nil, "", "", 80); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = st.StaleFact(ctx, []int64{1, msft.ID, es.ID}, 50); err != nil || ok {
		t.Fatal(err, ok)
	}

	lone, _, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Stock, Display: "NVDA", ProviderSymbol: "NVDA", Name: "Nvidia"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveFromGroup(ctx, gid, lone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAlert(ctx, lone.ID, "above", "once", 10); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertQuote(ctx, lone.ID, 100, nil, nil, nil, nil, nil, nil, nil, 1, false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRange(ctx, lone.ID, "2024-01-02", 1, 2, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFacts(ctx, lone.ID, &cap, nil, nil, nil, "2026-02-20", "bmo", 90); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range groupNamed(t, board, "Notable").Rows {
		if row.Display != "NVDA" {
			continue
		}
		found = row.MarketCap != nil && *row.MarketCap == cap && row.PeTTM == nil && row.EarningsDate == "2026-02-20" && row.EarningsHour == "bmo"
	}
	if !found {
		t.Fatalf("notable %+v", groupNamed(t, board, "Notable").Rows)
	}
}
