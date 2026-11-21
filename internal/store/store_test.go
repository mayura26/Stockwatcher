package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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
	if len(board.Groups) != 1 || board.Groups[0].Name != "Watch" {
		t.Fatalf("%+v", board.Groups)
	}
	gid := board.Groups[0].ID
	sym, created, err := st.AddToGroup(ctx, gid, NewSymbol{AssetClass: market.Future, Display: "ES", ProviderSymbol: "ES=F", Name: "E-mini S&P 500"})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	day := 1.25
	if err := st.UpsertQuote(ctx, sym.ID, 5800, nil, &day, nil, nil, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertBars(ctx, sym.ID, []market.Bar{{Day: "2024-01-02", Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 3}}); err != nil {
		t.Fatal(err)
	}
	board, err = st.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Groups[0].Rows) != 1 || board.Groups[0].Rows[0].Display != "ES" || board.Groups[0].Rows[0].Price == nil || *board.Groups[0].Rows[0].Price != 5800 {
		t.Fatalf("%+v", board.Groups[0].Rows)
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
	if legacy.Repeat != "once" || legacy.Status != "armed" {
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
