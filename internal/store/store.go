package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stockwatcher/internal/market"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrLastGroup  = errors.New("last group")
	ErrBadReorder = errors.New("bad reorder")
)

// Store is the SQLite cache the UI reads. Providers never sit on the request path
// except a chart open against an empty history.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.ensureGroup(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func sqliteDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range filepath.ToSlash(abs) {
		switch r {
		case ' ':
			b.WriteString("%20")
		case '?':
			b.WriteString("%3F")
		case '#':
			b.WriteString("%23")
		case '%':
			b.WriteString("%25")
		default:
			b.WriteRune(r)
		}
	}
	return "file:" + b.String() + "?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS symbols (
  id INTEGER PRIMARY KEY,
  asset_class TEXT NOT NULL CHECK (asset_class IN ('stock','future','crypto')),
  display TEXT NOT NULL,
  provider_symbol TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  bars_synced_at INTEGER,
  UNIQUE (asset_class, provider_symbol)
);
CREATE TABLE IF NOT EXISTS groups (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  position INTEGER NOT NULL,
  color TEXT NOT NULL DEFAULT '#c6a15b',
  icon TEXT NOT NULL DEFAULT 'mark'
);
CREATE TABLE IF NOT EXISTS group_symbols (
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  symbol_id INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  PRIMARY KEY (group_id, symbol_id)
);
CREATE TABLE IF NOT EXISTS quotes (
  symbol_id INTEGER PRIMARY KEY REFERENCES symbols(id) ON DELETE CASCADE,
  price REAL NOT NULL,
  prev_close REAL,
  change_d REAL,
  change_w REAL,
  change_m REAL,
  change_1h REAL,
  change_4h REAL,
  sparkline TEXT,
  ext_session TEXT,
  ext_pct REAL,
  as_of INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS bars (
  symbol_id INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
  day TEXT NOT NULL,
  open REAL NOT NULL,
  high REAL NOT NULL,
  low REAL NOT NULL,
  close REAL NOT NULL,
  volume REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (symbol_id, day)
);
CREATE TABLE IF NOT EXISTS alerts (
  id INTEGER PRIMARY KEY,
  symbol_id INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
  direction TEXT NOT NULL CHECK (direction IN ('above','below')),
  level REAL NOT NULL,
  repeat TEXT NOT NULL DEFAULT 'once' CHECK (repeat IN ('once','continuous')),
  status TEXT NOT NULL DEFAULT 'armed' CHECK (status IN ('armed','triggered')),
  last_price REAL,
  triggered_at INTEGER,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS push_subscriptions (
  id INTEGER PRIMARY KEY,
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_bars_symbol_day ON bars(symbol_id, day);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status, symbol_id);
`)
	if err != nil {
		return err
	}
	if err := s.ensureAlertRepeat(); err != nil {
		return err
	}
	if err := s.ensureGroupAppearance(); err != nil {
		return err
	}
	if err := s.ensureQuoteHourly(); err != nil {
		return err
	}
	if err := s.ensureQuoteSparkline(); err != nil {
		return err
	}
	if err := s.ensureQuoteExtended(); err != nil {
		return err
	}
	if err := s.ensureRanges(); err != nil {
		return err
	}
	if err := s.ensureOwned(); err != nil {
		return err
	}
	if err := s.ensureFacts(); err != nil {
		return err
	}
	return s.ensureLogos()
}

// ensureRanges stores the expected band for the last completed session.
// The board reads it; the tick rewrites it once that session changes.
func (s *Store) ensureRanges() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS ranges (
  symbol_id INTEGER PRIMARY KEY REFERENCES symbols(id) ON DELETE CASCADE,
  basis_day TEXT NOT NULL,
  low REAL NOT NULL,
  high REAL NOT NULL,
  computed_at INTEGER NOT NULL
)`)
	return err
}

// ensureFacts stores the slow company snapshot shown on an expanded stock card.
func (s *Store) ensureFacts() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS facts (
  symbol_id INTEGER PRIMARY KEY REFERENCES symbols(id) ON DELETE CASCADE,
  market_cap REAL,
  pe_ttm REAL,
  revenue_ttm REAL,
  earnings_ttm REAL,
  earnings_date TEXT,
  earnings_hour TEXT,
  fetched_at INTEGER NOT NULL
)`)
	return err
}

// ensureLogos stores one fetched mark per symbol. A null body is a recorded miss.
func (s *Store) ensureLogos() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS symbol_logos (
  symbol_id INTEGER PRIMARY KEY REFERENCES symbols(id) ON DELETE CASCADE,
  body BLOB,
  media_type TEXT,
  checked_at INTEGER NOT NULL
)`)
	return err
}

// ensureOwned stores the starred holdings. Membership is independent of groups.
func (s *Store) ensureOwned() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS owned (
  symbol_id INTEGER PRIMARY KEY REFERENCES symbols(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  created_at INTEGER NOT NULL
)`)
	return err
}

// ensureAlertRepeat adds the repeat column on databases created before one-off
// and continuous alerts. Existing rows become one-off.
func (s *Store) ensureAlertRepeat() error {
	rows, err := s.db.Query(`PRAGMA table_info(alerts)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "repeat" {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE alerts ADD COLUMN repeat TEXT NOT NULL DEFAULT 'once' CHECK (repeat IN ('once','continuous'))`)
	return err
}

// ensureGroupAppearance adds color and icon on databases created when a group
// was only a name. Existing rows become brass with the mark icon.
func (s *Store) ensureGroupAppearance() error {
	rows, err := s.db.Query(`PRAGMA table_info(groups)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasColor, hasIcon := false, false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		switch name {
		case "color":
			hasColor = true
		case "icon":
			hasIcon = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasColor {
		if _, err := s.db.Exec(`ALTER TABLE groups ADD COLUMN color TEXT NOT NULL DEFAULT '#c6a15b'`); err != nil {
			return err
		}
	}
	if !hasIcon {
		if _, err := s.db.Exec(`ALTER TABLE groups ADD COLUMN icon TEXT NOT NULL DEFAULT 'mark'`); err != nil {
			return err
		}
	}
	return nil
}

// ensureQuoteHourly adds 1H and 4H percents on databases created before them.
func (s *Store) ensureQuoteHourly() error {
	rows, err := s.db.Query(`PRAGMA table_info(quotes)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has1, has4 := false, false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		switch name {
		case "change_1h":
			has1 = true
		case "change_4h":
			has4 = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !has1 {
		if _, err := s.db.Exec(`ALTER TABLE quotes ADD COLUMN change_1h REAL`); err != nil {
			return err
		}
	}
	if !has4 {
		if _, err := s.db.Exec(`ALTER TABLE quotes ADD COLUMN change_4h REAL`); err != nil {
			return err
		}
	}
	return nil
}

// ensureQuoteSparkline adds the last-session closes on databases created before them.
func (s *Store) ensureQuoteSparkline() error {
	rows, err := s.db.Query(`PRAGMA table_info(quotes)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "sparkline" {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE quotes ADD COLUMN sparkline TEXT`)
	return err
}

// ensureQuoteExtended adds the premarket or after-hours percent on databases created before it.
func (s *Store) ensureQuoteExtended() error {
	rows, err := s.db.Query(`PRAGMA table_info(quotes)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasSession, hasPct := false, false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		switch name {
		case "ext_session":
			hasSession = true
		case "ext_pct":
			hasPct = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasSession {
		if _, err := s.db.Exec(`ALTER TABLE quotes ADD COLUMN ext_session TEXT`); err != nil {
			return err
		}
	}
	if !hasPct {
		if _, err := s.db.Exec(`ALTER TABLE quotes ADD COLUMN ext_pct REAL`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureGroup() error {
	_, err := s.db.Exec(`INSERT INTO groups(name, position) SELECT 'Watch', 0 WHERE NOT EXISTS (SELECT 1 FROM groups)`)
	return err
}

const (
	NotableGroupID int64 = 0
	OwnedGroupID   int64 = -1
)

type Row struct {
	ID             int64     `json:"id"`
	Display        string    `json:"display"`
	Name           string    `json:"name"`
	AssetClass     string    `json:"asset_class"`
	ProviderSymbol string    `json:"provider_symbol"`
	Price          *float64  `json:"price"`
	ChangeD        *float64  `json:"change_d"`
	ChangeW        *float64  `json:"change_w"`
	ChangeM        *float64  `json:"change_m"`
	ChangeH1       *float64  `json:"change_1h"`
	ChangeH4       *float64  `json:"change_4h"`
	Sparkline      []float64 `json:"sparkline,omitempty"`
	ExtSession     string    `json:"ext_session,omitempty"`
	ExtPct         *float64  `json:"ext_pct,omitempty"`
	AsOf           *int64    `json:"as_of"`
	Position       int       `json:"position"`
	RangeLow       *float64  `json:"range_low,omitempty"`
	RangeHigh      *float64  `json:"range_high,omitempty"`
	Owned          bool      `json:"owned,omitempty"`
	MarketCap      *float64  `json:"market_cap,omitempty"`
	PeTTM          *float64  `json:"pe_ttm,omitempty"`
	RevenueTTM     *float64  `json:"revenue_ttm,omitempty"`
	EarningsTTM    *float64  `json:"earnings_ttm,omitempty"`
	EarningsDate   string    `json:"earnings_date,omitempty"`
	EarningsHour   string    `json:"earnings_hour,omitempty"`
	HasLogo        bool      `json:"has_logo,omitempty"`
}

type Group struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	Position  int    `json:"position"`
	Kind      string `json:"kind,omitempty"`
	Synthetic bool   `json:"synthetic,omitempty"`
	Rows      []Row  `json:"rows"`
}

type Board struct {
	Groups      []Group `json:"groups"`
	ServerTime  int64   `json:"server_time"`
	ArmedAlerts int     `json:"armed_alerts"`
}

func (s *Store) Board(ctx context.Context) (Board, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT g.id, g.name, g.color, g.icon, g.position,
       s.id, s.display, s.name, s.asset_class, s.provider_symbol,
       q.price, q.change_d, q.change_w, q.change_m, q.change_1h, q.change_4h, q.sparkline, q.as_of,
       q.ext_session, q.ext_pct, gs.position, r.low, r.high,
       f.market_cap, f.pe_ttm, f.revenue_ttm, f.earnings_ttm, f.earnings_date, f.earnings_hour,
       EXISTS(SELECT 1 FROM symbol_logos l WHERE l.symbol_id = s.id AND l.body IS NOT NULL)
FROM groups g
LEFT JOIN group_symbols gs ON gs.group_id = g.id
LEFT JOIN symbols s ON s.id = gs.symbol_id
LEFT JOIN quotes q ON q.symbol_id = s.id
LEFT JOIN ranges r ON r.symbol_id = s.id
LEFT JOIN facts f ON f.symbol_id = s.id
ORDER BY g.position, g.id, gs.position, s.id`)
	if err != nil {
		return Board{}, err
	}
	defer rows.Close()
	board := Board{Groups: []Group{}, ServerTime: time.Now().Unix()}
	index := map[int64]int{}
	for rows.Next() {
		var (
			gid, gpos            int64
			gname, gcolor, gicon string
			sid                  sql.NullInt64
			display, name, cls   sql.NullString
			provider             sql.NullString
			price, cd, cw, cm    sql.NullFloat64
			h1, h4               sql.NullFloat64
			spark                sql.NullString
			asOf                 sql.NullInt64
			extSess              sql.NullString
			extPct               sql.NullFloat64
			spos                 sql.NullInt64
			rlow, rhigh          sql.NullFloat64
			cap, pe, rev, earn   sql.NullFloat64
			earnDate, earnHour   sql.NullString
			has                  int
		)
		if err := rows.Scan(&gid, &gname, &gcolor, &gicon, &gpos, &sid, &display, &name, &cls, &provider, &price, &cd, &cw, &cm, &h1, &h4, &spark, &asOf, &extSess, &extPct, &spos, &rlow, &rhigh, &cap, &pe, &rev, &earn, &earnDate, &earnHour, &has); err != nil {
			return Board{}, err
		}
		i, ok := index[gid]
		if !ok {
			i = len(board.Groups)
			index[gid] = i
			board.Groups = append(board.Groups, Group{ID: gid, Name: gname, Color: gcolor, Icon: gicon, Position: int(gpos), Rows: []Row{}})
		}
		if !sid.Valid {
			continue
		}
		pos := 0
		if spos.Valid {
			pos = int(spos.Int64)
		}
		row := Row{
			ID:             sid.Int64,
			Display:        display.String,
			Name:           name.String,
			AssetClass:     cls.String,
			ProviderSymbol: provider.String,
			Price:          floatPtr(price),
			ChangeD:        floatPtr(cd),
			ChangeW:        floatPtr(cw),
			ChangeM:        floatPtr(cm),
			ChangeH1:       floatPtr(h1),
			ChangeH4:       floatPtr(h4),
			Sparkline:      decodeSpark(spark),
			AsOf:           intPtr(asOf),
			ExtSession:     extSess.String,
			ExtPct:         floatPtr(extPct),
			Position:       pos,
			RangeLow:       floatPtr(rlow),
			RangeHigh:      floatPtr(rhigh),
		}
		setFacts(&row, cap, pe, rev, earn, earnDate, earnHour)
		row.HasLogo = has != 0
		board.Groups[i].Rows = append(board.Groups[i].Rows, row)
	}
	if err := rows.Err(); err != nil {
		return Board{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'armed'`).Scan(&board.ArmedAlerts); err != nil {
		return Board{}, err
	}
	owned, err := s.ownedRows(ctx)
	if err != nil {
		return Board{}, err
	}
	stampOwned(board.Groups, owned)
	notable, err := s.notableGroup(ctx, board.Groups, owned)
	if err != nil {
		return Board{}, err
	}
	board.Groups = append([]Group{notable, ownedGroup(owned)}, board.Groups...)
	return board, nil
}

// notableGroup is the pinned list of watched names that have traded outside
// the cached band. It is not a stored group. Watched names are the same set
// the tick refreshes: anything on a board, an armed alert with no group, or
// an owned name with no group.
func (s *Store) notableGroup(ctx context.Context, groups []Group, owned []Row) (Group, error) {
	seen := map[int64]struct{}{}
	var candidates []Row
	for _, g := range groups {
		for _, row := range g.Rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			candidates = append(candidates, row)
		}
	}
	extra, err := s.alertOnlyRows(ctx)
	if err != nil {
		return Group{}, err
	}
	for _, row := range extra {
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		candidates = append(candidates, row)
	}
	for _, row := range owned {
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		candidates = append(candidates, row)
	}
	var rows []Row
	for _, row := range candidates {
		if !outsideBand(row) {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return excursion(rows[i]) > excursion(rows[j])
	})
	if rows == nil {
		rows = []Row{}
	}
	ownedSet := map[int64]struct{}{}
	for _, row := range owned {
		ownedSet[row.ID] = struct{}{}
	}
	for i := range rows {
		rows[i].Position = i
		if _, ok := ownedSet[rows[i].ID]; ok {
			rows[i].Owned = true
		}
	}
	return Group{
		ID:        NotableGroupID,
		Name:      "Notable",
		Color:     "#c56b52",
		Icon:      "pulse",
		Kind:      "notable",
		Synthetic: true,
		Rows:      rows,
	}, nil
}

func (s *Store) alertOnlyRows(ctx context.Context) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.display, s.name, s.asset_class, s.provider_symbol,
       q.price, q.change_d, q.change_w, q.change_m, q.change_1h, q.change_4h, q.sparkline, q.as_of,
       q.ext_session, q.ext_pct, r.low, r.high,
       f.market_cap, f.pe_ttm, f.revenue_ttm, f.earnings_ttm, f.earnings_date, f.earnings_hour,
       EXISTS(SELECT 1 FROM symbol_logos l WHERE l.symbol_id = s.id AND l.body IS NOT NULL)
FROM symbols s
LEFT JOIN quotes q ON q.symbol_id = s.id
LEFT JOIN ranges r ON r.symbol_id = s.id
LEFT JOIN facts f ON f.symbol_id = s.id
WHERE EXISTS (SELECT 1 FROM alerts a WHERE a.symbol_id = s.id AND a.status = 'armed')
  AND NOT EXISTS (SELECT 1 FROM group_symbols gs WHERE gs.symbol_id = s.id)
ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var (
			row                 Row
			price, cd, cw, cm   sql.NullFloat64
			h1, h4, rlow, rhigh sql.NullFloat64
			spark               sql.NullString
			asOf                sql.NullInt64
			extSess             sql.NullString
			extPct              sql.NullFloat64
			cap, pe, rev, earn  sql.NullFloat64
			earnDate, earnHour  sql.NullString
			has                 int
		)
		if err := rows.Scan(&row.ID, &row.Display, &row.Name, &row.AssetClass, &row.ProviderSymbol, &price, &cd, &cw, &cm, &h1, &h4, &spark, &asOf, &extSess, &extPct, &rlow, &rhigh, &cap, &pe, &rev, &earn, &earnDate, &earnHour, &has); err != nil {
			return nil, err
		}
		row.Price = floatPtr(price)
		row.ChangeD = floatPtr(cd)
		row.ChangeW = floatPtr(cw)
		row.ChangeM = floatPtr(cm)
		row.ChangeH1 = floatPtr(h1)
		row.ChangeH4 = floatPtr(h4)
		row.Sparkline = decodeSpark(spark)
		row.AsOf = intPtr(asOf)
		row.ExtSession = extSess.String
		row.ExtPct = floatPtr(extPct)
		row.RangeLow = floatPtr(rlow)
		row.RangeHigh = floatPtr(rhigh)
		setFacts(&row, cap, pe, rev, earn, earnDate, earnHour)
		row.HasLogo = has != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) ownedRows(ctx context.Context) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.display, s.name, s.asset_class, s.provider_symbol,
       q.price, q.change_d, q.change_w, q.change_m, q.change_1h, q.change_4h, q.sparkline, q.as_of,
       q.ext_session, q.ext_pct, r.low, r.high,
       f.market_cap, f.pe_ttm, f.revenue_ttm, f.earnings_ttm, f.earnings_date, f.earnings_hour,
       EXISTS(SELECT 1 FROM symbol_logos l WHERE l.symbol_id = s.id AND l.body IS NOT NULL)
FROM owned o
JOIN symbols s ON s.id = o.symbol_id
LEFT JOIN quotes q ON q.symbol_id = s.id
LEFT JOIN ranges r ON r.symbol_id = s.id
LEFT JOIN facts f ON f.symbol_id = s.id
ORDER BY o.position, s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var (
			row                 Row
			price, cd, cw, cm   sql.NullFloat64
			h1, h4, rlow, rhigh sql.NullFloat64
			spark               sql.NullString
			asOf                sql.NullInt64
			extSess             sql.NullString
			extPct              sql.NullFloat64
			cap, pe, rev, earn  sql.NullFloat64
			earnDate, earnHour  sql.NullString
			has                 int
		)
		if err := rows.Scan(&row.ID, &row.Display, &row.Name, &row.AssetClass, &row.ProviderSymbol, &price, &cd, &cw, &cm, &h1, &h4, &spark, &asOf, &extSess, &extPct, &rlow, &rhigh, &cap, &pe, &rev, &earn, &earnDate, &earnHour, &has); err != nil {
			return nil, err
		}
		row.Price = floatPtr(price)
		row.ChangeD = floatPtr(cd)
		row.ChangeW = floatPtr(cw)
		row.ChangeM = floatPtr(cm)
		row.ChangeH1 = floatPtr(h1)
		row.ChangeH4 = floatPtr(h4)
		row.Sparkline = decodeSpark(spark)
		row.AsOf = intPtr(asOf)
		row.ExtSession = extSess.String
		row.ExtPct = floatPtr(extPct)
		row.RangeLow = floatPtr(rlow)
		row.RangeHigh = floatPtr(rhigh)
		setFacts(&row, cap, pe, rev, earn, earnDate, earnHour)
		row.HasLogo = has != 0
		row.Owned = true
		out = append(out, row)
	}
	return out, rows.Err()
}

func ownedGroup(rows []Row) Group {
	if rows == nil {
		rows = []Row{}
	}
	for i := range rows {
		rows[i].Owned = true
		rows[i].Position = i
	}
	return Group{
		ID:        OwnedGroupID,
		Name:      "Owned",
		Color:     "#8eae8a",
		Icon:      "coin",
		Kind:      "owned",
		Synthetic: true,
		Rows:      rows,
	}
}

func stampOwned(groups []Group, owned []Row) {
	set := map[int64]struct{}{}
	for _, row := range owned {
		set[row.ID] = struct{}{}
	}
	for i := range groups {
		for j := range groups[i].Rows {
			if _, ok := set[groups[i].Rows[j].ID]; ok {
				groups[i].Rows[j].Owned = true
			}
		}
	}
}

func outsideBand(row Row) bool {
	if row.RangeLow == nil || row.RangeHigh == nil {
		return false
	}
	low, high := *row.RangeLow, *row.RangeHigh
	if !(high > low) {
		return false
	}
	if row.Price != nil && (*row.Price < low || *row.Price > high) {
		return true
	}
	for _, p := range row.Sparkline {
		if p < low || p > high {
			return true
		}
	}
	return dayBeyond(row.ChangeD, low, high)
}

func dayBeyond(change *float64, low, high float64) bool {
	if change == nil {
		return false
	}
	expected := bandPercent(low, high)
	return expected > 0 && (*change > expected || *change < -expected)
}

func bandPercent(low, high float64) float64 {
	mid := (low + high) / 2
	if mid <= 0 || high <= low {
		return 0
	}
	return (high/mid - 1) * 100
}

func excursion(row Row) float64 {
	low, high := *row.RangeLow, *row.RangeHigh
	width := high - low
	if width <= 0 {
		return 0
	}
	best := 0.0
	consider := func(p float64) {
		var d float64
		if p > high {
			d = (p - high) / width
		} else if p < low {
			d = (low - p) / width
		}
		if d > best {
			best = d
		}
	}
	if row.Price != nil {
		consider(*row.Price)
	}
	for _, p := range row.Sparkline {
		consider(p)
	}
	if row.ChangeD != nil {
		expected := bandPercent(low, high)
		if expected > 0 {
			over := *row.ChangeD
			if over < 0 {
				over = -over
			}
			over -= expected
			if over > 0 {
				d := over / (2 * expected)
				if d > best {
					best = d
				}
			}
		}
	}
	return best
}

func decodeSpark(n sql.NullString) []float64 {
	if !n.Valid || n.String == "" {
		return nil
	}
	var out []float64
	if err := json.Unmarshal([]byte(n.String), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func setFacts(row *Row, cap, pe, rev, earn sql.NullFloat64, date, hour sql.NullString) {
	row.MarketCap = floatPtr(cap)
	row.PeTTM = floatPtr(pe)
	row.RevenueTTM = floatPtr(rev)
	row.EarningsTTM = floatPtr(earn)
	row.EarningsDate = date.String
	row.EarningsHour = hour.String
}

func floatArg(n *float64) any {
	if n == nil {
		return nil
	}
	return *n
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

type NewSymbol struct {
	AssetClass     string
	Display        string
	ProviderSymbol string
	Name           string
}

type Symbol struct {
	ID             int64
	AssetClass     string
	Display        string
	ProviderSymbol string
	Name           string
	BarsSyncedAt   *int64
}

func (s *Store) AddToGroup(ctx context.Context, groupID int64, in NewSymbol) (Symbol, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Symbol{}, false, err
	}
	defer tx.Rollback()
	var gid int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id = ?`, groupID).Scan(&gid)
	if errors.Is(err, sql.ErrNoRows) {
		return Symbol{}, false, ErrNotFound
	}
	if err != nil {
		return Symbol{}, false, err
	}
	sym, created, err := upsertSymbol(ctx, tx, in)
	if err != nil {
		return Symbol{}, false, err
	}
	var pos int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM group_symbols WHERE group_id = ?`, groupID).Scan(&pos); err != nil {
		return Symbol{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO group_symbols(group_id, symbol_id, position) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, groupID, sym.ID, pos); err != nil {
		return Symbol{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Symbol{}, false, err
	}
	return sym, created, nil
}

func upsertSymbol(ctx context.Context, tx *sql.Tx, in NewSymbol) (Symbol, bool, error) {
	var sym Symbol
	var synced sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, asset_class, display, provider_symbol, name, bars_synced_at FROM symbols WHERE asset_class = ? AND provider_symbol = ?`, in.AssetClass, in.ProviderSymbol).
		Scan(&sym.ID, &sym.AssetClass, &sym.Display, &sym.ProviderSymbol, &sym.Name, &synced)
	if err == nil {
		sym.BarsSyncedAt = intPtr(synced)
		if in.Name != "" && in.Name != sym.Name {
			if _, err := tx.ExecContext(ctx, `UPDATE symbols SET name = ?, display = ? WHERE id = ?`, in.Name, in.Display, sym.ID); err != nil {
				return Symbol{}, false, err
			}
			sym.Name = in.Name
			sym.Display = in.Display
		}
		return sym, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Symbol{}, false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO symbols(asset_class, display, provider_symbol, name) VALUES (?, ?, ?, ?)`, in.AssetClass, in.Display, in.ProviderSymbol, in.Name)
	if err != nil {
		return Symbol{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Symbol{}, false, err
	}
	return Symbol{ID: id, AssetClass: in.AssetClass, Display: in.Display, ProviderSymbol: in.ProviderSymbol, Name: in.Name}, true, nil
}

func (s *Store) CreateGroup(ctx context.Context, name, color, icon string) (Group, error) {
	var pos int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM groups`).Scan(&pos); err != nil {
		return Group{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO groups(name, position, color, icon) VALUES (?, ?, ?, ?)`, name, pos, color, icon)
	if err != nil {
		return Group{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Group{}, err
	}
	return Group{ID: id, Name: name, Color: color, Icon: icon, Position: pos, Rows: []Row{}}, nil
}

func (s *Store) RenameGroup(ctx context.Context, id int64, name, color, icon string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE groups SET name = ?, color = ?, icon = ? WHERE id = ?`, name, color, icon, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups`).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastGroup
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ReorderGroups(ctx context.Context, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups`).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return ErrBadReorder
	}
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, `UPDATE groups SET position = ? WHERE id = ?`, i, id)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrBadReorder
		}
	}
	return tx.Commit()
}

func (s *Store) RemoveFromGroup(ctx context.Context, groupID, symbolID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM group_symbols WHERE group_id = ? AND symbol_id = ?`, groupID, symbolID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetOwned stars or unstars a symbol. Starring an owned symbol leaves its place.
// Unstarring a symbol that is not owned returns ErrNotFound.
func (s *Store) SetOwned(ctx context.Context, symbolID int64, owned bool) error {
	if !owned {
		res, err := s.db.ExecContext(ctx, `DELETE FROM owned WHERE symbol_id = ?`, symbolID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	}
	if _, err := s.Symbol(ctx, symbolID); err != nil {
		return err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM owned WHERE symbol_id = ?`, symbolID).Scan(&n); err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var pos int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM owned`).Scan(&pos); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO owned(symbol_id, position, created_at) VALUES (?, ?, ?)`, symbolID, pos, time.Now().Unix())
	return err
}

func (s *Store) ReorderSymbols(ctx context.Context, groupID int64, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_symbols WHERE group_id = ?`, groupID).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return ErrBadReorder
	}
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, `UPDATE group_symbols SET position = ? WHERE group_id = ? AND symbol_id = ?`, i, groupID, id)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrBadReorder
		}
	}
	return tx.Commit()
}

func (s *Store) Symbol(ctx context.Context, id int64) (Symbol, error) {
	var sym Symbol
	var synced sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, asset_class, display, provider_symbol, name, bars_synced_at FROM symbols WHERE id = ?`, id).
		Scan(&sym.ID, &sym.AssetClass, &sym.Display, &sym.ProviderSymbol, &sym.Name, &synced)
	if errors.Is(err, sql.ErrNoRows) {
		return Symbol{}, ErrNotFound
	}
	if err != nil {
		return Symbol{}, err
	}
	sym.BarsSyncedAt = intPtr(synced)
	return sym, nil
}

func (s *Store) BarCount(ctx context.Context, symbolID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bars WHERE symbol_id = ?`, symbolID).Scan(&n)
	return n, err
}

func (s *Store) Bars(ctx context.Context, symbolID int64) ([]market.Bar, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, open, high, low, close, volume FROM bars WHERE symbol_id = ? ORDER BY day`, symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bars []market.Bar
	for rows.Next() {
		var b market.Bar
		if err := rows.Scan(&b.Day, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			return nil, err
		}
		bars = append(bars, b)
	}
	return bars, rows.Err()
}

func (s *Store) UpsertBars(ctx context.Context, symbolID int64, bars []market.Bar) error {
	if len(bars) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO bars(symbol_id, day, open, high, low, close, volume) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(symbol_id, day) DO UPDATE SET
  open = excluded.open, high = excluded.high, low = excluded.low, close = excluded.close, volume = excluded.volume`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, b := range bars {
		if len(b.Day) != 10 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, symbolID, b.Day, b.Open, b.High, b.Low, b.Close, b.Volume); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE symbols SET bars_synced_at = ? WHERE id = ?`, time.Now().Unix(), symbolID); err != nil {
		return err
	}
	return tx.Commit()
}

// Range is the cached expected band for one symbol.
type Range struct {
	SymbolID   int64
	BasisDay   string
	Low        float64
	High       float64
	ComputedAt int64
}

func (s *Store) UpsertRange(ctx context.Context, symbolID int64, basis string, low, high float64, at int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO ranges(symbol_id, basis_day, low, high, computed_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(symbol_id) DO UPDATE SET
  basis_day = excluded.basis_day,
  low = excluded.low,
  high = excluded.high,
  computed_at = excluded.computed_at`, symbolID, basis, low, high, at)
	return err
}

func (s *Store) Range(ctx context.Context, symbolID int64) (Range, error) {
	var r Range
	err := s.db.QueryRowContext(ctx, `SELECT symbol_id, basis_day, low, high, computed_at FROM ranges WHERE symbol_id = ?`, symbolID).
		Scan(&r.SymbolID, &r.BasisDay, &r.Low, &r.High, &r.ComputedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Range{}, ErrNotFound
	}
	return r, err
}

// RangeStatus is the latest two session dates plus the cached basis, so the
// tick can decide whether a band is current without reading every bar.
type RangeStatus struct {
	SymbolID   int64
	AssetClass string
	LastDay    string
	PrevDay    string
	BasisDay   string
	ComputedAt int64
}

func (s *Store) RangeStatuses(ctx context.Context) ([]RangeStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.asset_class,
       COALESCE((SELECT MAX(day) FROM bars b WHERE b.symbol_id = s.id), ''),
       COALESCE((SELECT MAX(day) FROM bars b WHERE b.symbol_id = s.id AND day < (
         SELECT MAX(day) FROM bars b2 WHERE b2.symbol_id = s.id
       )), ''),
       COALESCE(r.basis_day, ''),
       COALESCE(r.computed_at, 0)
FROM symbols s
LEFT JOIN ranges r ON r.symbol_id = s.id
WHERE EXISTS (SELECT 1 FROM group_symbols gs WHERE gs.symbol_id = s.id)
   OR EXISTS (SELECT 1 FROM alerts a WHERE a.symbol_id = s.id AND a.status = 'armed')
   OR EXISTS (SELECT 1 FROM owned o WHERE o.symbol_id = s.id)
ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RangeStatus
	for rows.Next() {
		var st RangeStatus
		if err := rows.Scan(&st.SymbolID, &st.AssetClass, &st.LastDay, &st.PrevDay, &st.BasisDay, &st.ComputedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

type QuoteRow struct {
	Price      float64
	PrevClose  *float64
	ChangeD    *float64
	ChangeW    *float64
	ChangeM    *float64
	ChangeH1   *float64
	ChangeH4   *float64
	AsOf       int64
	ExtSession string
	ExtPct     *float64
}

func (s *Store) Quote(ctx context.Context, symbolID int64) (QuoteRow, error) {
	var q QuoteRow
	var prev, d, w, m, h1, h4, extPct sql.NullFloat64
	var extSess sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT price, prev_close, change_d, change_w, change_m, change_1h, change_4h, as_of, ext_session, ext_pct FROM quotes WHERE symbol_id = ?`, symbolID).
		Scan(&q.Price, &prev, &d, &w, &m, &h1, &h4, &q.AsOf, &extSess, &extPct)
	if errors.Is(err, sql.ErrNoRows) {
		return QuoteRow{}, ErrNotFound
	}
	if err != nil {
		return QuoteRow{}, err
	}
	q.PrevClose = floatPtr(prev)
	q.ChangeD = floatPtr(d)
	q.ChangeW = floatPtr(w)
	q.ChangeM = floatPtr(m)
	q.ChangeH1 = floatPtr(h1)
	q.ChangeH4 = floatPtr(h4)
	q.ExtSession = extSess.String
	q.ExtPct = floatPtr(extPct)
	return q, nil
}

// Logo is a cached mark. Body is empty when the row is only a recorded miss.
type Logo struct {
	Body      []byte
	MediaType string
}

// PutLogo writes a fetched image, or a miss when body is empty.
func (s *Store) PutLogo(ctx context.Context, symbolID int64, body []byte, mediaType string, checkedAt int64) error {
	var blob, media any
	if len(body) > 0 {
		blob = body
		media = mediaType
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO symbol_logos(symbol_id, body, media_type, checked_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(symbol_id) DO UPDATE SET
  body = excluded.body,
  media_type = excluded.media_type,
  checked_at = excluded.checked_at`, symbolID, blob, media, checkedAt)
	return err
}

// Logo returns the stored image. A miss and a symbol that has never been checked are both not found.
func (s *Store) Logo(ctx context.Context, symbolID int64) (Logo, error) {
	var logo Logo
	var media sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT body, media_type FROM symbol_logos WHERE symbol_id = ? AND body IS NOT NULL`, symbolID).Scan(&logo.Body, &media)
	if errors.Is(err, sql.ErrNoRows) {
		return Logo{}, ErrNotFound
	}
	if err != nil {
		return Logo{}, err
	}
	logo.MediaType = media.String
	return logo, nil
}

// SymbolsNeedingLogo is every symbol with no cached mark, or a miss older than olderThan.
// Missing rows come first, then the oldest check, then the lowest id.
func (s *Store) SymbolsNeedingLogo(ctx context.Context, olderThan int64) ([]Symbol, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.asset_class, s.display, s.provider_symbol, s.name
FROM symbols s
LEFT JOIN symbol_logos l ON l.symbol_id = s.id
WHERE l.symbol_id IS NULL OR (l.body IS NULL AND l.checked_at < ?)
ORDER BY CASE WHEN l.symbol_id IS NULL THEN 0 ELSE 1 END, l.checked_at, s.id`, olderThan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.AssetClass, &sym.Display, &sym.ProviderSymbol, &sym.Name); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// UpsertFacts replaces the company snapshot. A failed fetch must not call this,
// so the previous row stays on the card.
func (s *Store) UpsertFacts(ctx context.Context, symbolID int64, marketCap, pe, revenue, earnings *float64, earningsDate, earningsHour string, fetchedAt int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO facts(symbol_id, market_cap, pe_ttm, revenue_ttm, earnings_ttm, earnings_date, earnings_hour, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(symbol_id) DO UPDATE SET
  market_cap = excluded.market_cap,
  pe_ttm = excluded.pe_ttm,
  revenue_ttm = excluded.revenue_ttm,
  earnings_ttm = excluded.earnings_ttm,
  earnings_date = excluded.earnings_date,
  earnings_hour = excluded.earnings_hour,
  fetched_at = excluded.fetched_at`,
		symbolID, floatArg(marketCap), floatArg(pe), floatArg(revenue), floatArg(earnings), earningsDate, earningsHour, fetchedAt)
	return err
}

// StaleFact is the stock in ids whose snapshot is missing or older than olderThan.
// Missing rows come first, then the oldest fetch, then the lowest id.
func (s *Store) StaleFact(ctx context.Context, ids []int64, olderThan int64) (int64, bool, error) {
	if len(ids) == 0 {
		return 0, false, nil
	}
	args := make([]any, 0, len(ids)+1)
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, olderThan)
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT s.id
FROM symbols s
LEFT JOIN facts f ON f.symbol_id = s.id
WHERE s.asset_class = 'stock' AND s.id IN (`+strings.Join(placeholders, ",")+`)
  AND (f.symbol_id IS NULL OR f.fetched_at < ?)
ORDER BY CASE WHEN f.fetched_at IS NULL THEN 0 ELSE 1 END, f.fetched_at, s.id
LIMIT 1`, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// UpsertQuote writes a quote. hourly false keeps change_1h, change_4h, and the sparkline already stored.
// extSet false keeps ext_session and ext_pct. extSet true writes them, including a clear.
func (s *Store) UpsertQuote(ctx context.Context, symbolID int64, price float64, prev, d, w, m, h1, h4 *float64, spark []float64, asOf int64, hourly bool, extSession string, extPct *float64, extSet bool) error {
	flag := 0
	var sparkText any
	if hourly {
		flag = 1
		if spark == nil {
			spark = []float64{}
		}
		raw, err := json.Marshal(spark)
		if err != nil {
			return err
		}
		sparkText = string(raw)
	}
	extFlag := 0
	var session any
	var pct any
	if extSet {
		extFlag = 1
		if extSession != "" {
			session = extSession
		}
		if extPct != nil {
			pct = *extPct
		}
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO quotes(symbol_id, price, prev_close, change_d, change_w, change_m, change_1h, change_4h, sparkline, as_of, ext_session, ext_pct)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(symbol_id) DO UPDATE SET
  price = excluded.price,
  prev_close = excluded.prev_close,
  change_d = excluded.change_d,
  change_w = excluded.change_w,
  change_m = excluded.change_m,
  change_1h = CASE WHEN ? THEN excluded.change_1h ELSE quotes.change_1h END,
  change_4h = CASE WHEN ? THEN excluded.change_4h ELSE quotes.change_4h END,
  sparkline = CASE WHEN ? THEN excluded.sparkline ELSE quotes.sparkline END,
  ext_session = CASE WHEN ? THEN excluded.ext_session ELSE quotes.ext_session END,
  ext_pct = CASE WHEN ? THEN excluded.ext_pct ELSE quotes.ext_pct END,
  as_of = excluded.as_of`, symbolID, price, prev, d, w, m, h1, h4, sparkText, asOf, session, pct, flag, flag, flag, extFlag, extFlag)
	return err
}

// TickSymbol is one instrument the cron job may refresh.
type TickSymbol struct {
	ID             int64
	AssetClass     string
	Display        string
	ProviderSymbol string
	Name           string
	Armed          bool
	BarsSyncedAt   *int64
	LastBar        string
	BarCount       int
}

func (s *Store) TickSymbols(ctx context.Context) ([]TickSymbol, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.asset_class, s.display, s.provider_symbol, s.name, s.bars_synced_at,
       COALESCE((SELECT MAX(day) FROM bars b WHERE b.symbol_id = s.id), ''),
       (SELECT COUNT(*) FROM bars b WHERE b.symbol_id = s.id),
       EXISTS(SELECT 1 FROM alerts a WHERE a.symbol_id = s.id AND a.status = 'armed')
FROM symbols s
WHERE EXISTS (SELECT 1 FROM group_symbols gs WHERE gs.symbol_id = s.id)
   OR EXISTS (SELECT 1 FROM alerts a WHERE a.symbol_id = s.id AND a.status = 'armed')
   OR EXISTS (SELECT 1 FROM owned o WHERE o.symbol_id = s.id)
ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TickSymbol
	for rows.Next() {
		var sym TickSymbol
		var synced sql.NullInt64
		var armed int
		if err := rows.Scan(&sym.ID, &sym.AssetClass, &sym.Display, &sym.ProviderSymbol, &sym.Name, &synced, &sym.LastBar, &sym.BarCount, &armed); err != nil {
			return nil, err
		}
		sym.BarsSyncedAt = intPtr(synced)
		sym.Armed = armed == 1
		out = append(out, sym)
	}
	return out, rows.Err()
}

func (s *Store) SymbolsWithoutBars(ctx context.Context, limit int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id FROM symbols s
WHERE NOT EXISTS (SELECT 1 FROM bars b WHERE b.symbol_id = s.id)
  AND (
    EXISTS (SELECT 1 FROM group_symbols gs WHERE gs.symbol_id = s.id)
    OR EXISTS (SELECT 1 FROM alerts a WHERE a.symbol_id = s.id AND a.status = 'armed')
    OR EXISTS (SELECT 1 FROM owned o WHERE o.symbol_id = s.id)
  )
ORDER BY s.id
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) Meta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// TryLock takes a named lease. A crashed holder expires after ttl.
func (s *Store) TryLock(ctx context.Context, name string, ttl time.Duration) (bool, error) {
	now := time.Now().Unix()
	until := time.Now().Add(ttl).Unix()
	res, err := s.db.ExecContext(ctx, `
INSERT INTO meta(key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
WHERE CAST(meta.value AS INTEGER) < ?`, "lock:"+name, fmt.Sprintf("%d", until), now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) Unlock(ctx context.Context, name string) error {
	return s.SetMeta(ctx, "lock:"+name, "0")
}

type Alert struct {
	ID          int64    `json:"id"`
	SymbolID    int64    `json:"symbol_id"`
	Display     string   `json:"display"`
	Name        string   `json:"name"`
	AssetClass  string   `json:"asset_class"`
	Direction   string   `json:"direction"`
	Level       float64  `json:"level"`
	Repeat      string   `json:"repeat"`
	Status      string   `json:"status"`
	LastPrice   *float64 `json:"last_price"`
	TriggeredAt *int64   `json:"triggered_at"`
	CreatedAt   int64    `json:"created_at"`
	Price       *float64 `json:"price"`
}

func (s *Store) ListAlerts(ctx context.Context) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT a.id, a.symbol_id, s.display, s.name, s.asset_class, a.direction, a.level, a.repeat, a.status,
       a.last_price, a.triggered_at, a.created_at, q.price
FROM alerts a
JOIN symbols s ON s.id = a.symbol_id
LEFT JOIN quotes q ON q.symbol_id = s.id
ORDER BY CASE a.status WHEN 'armed' THEN 0 ELSE 1 END, a.created_at DESC, a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAlert(sc scanner) (Alert, error) {
	var a Alert
	var last, price sql.NullFloat64
	var triggered sql.NullInt64
	err := sc.Scan(&a.ID, &a.SymbolID, &a.Display, &a.Name, &a.AssetClass, &a.Direction, &a.Level, &a.Repeat, &a.Status, &last, &triggered, &a.CreatedAt, &price)
	if err != nil {
		return Alert{}, err
	}
	a.LastPrice = floatPtr(last)
	a.TriggeredAt = intPtr(triggered)
	a.Price = floatPtr(price)
	return a, nil
}

func (s *Store) Alert(ctx context.Context, id int64) (Alert, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT a.id, a.symbol_id, s.display, s.name, s.asset_class, a.direction, a.level, a.repeat, a.status,
       a.last_price, a.triggered_at, a.created_at, q.price
FROM alerts a
JOIN symbols s ON s.id = a.symbol_id
LEFT JOIN quotes q ON q.symbol_id = s.id
WHERE a.id = ?`, id)
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateAlert(ctx context.Context, symbolID int64, direction, repeat string, level float64) (Alert, error) {
	if _, err := s.Symbol(ctx, symbolID); err != nil {
		return Alert{}, err
	}
	if repeat == "" {
		repeat = "once"
	}
	var last *float64
	if q, err := s.Quote(ctx, symbolID); err == nil {
		v := q.Price
		last = &v
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `INSERT INTO alerts(symbol_id, direction, level, repeat, status, last_price, created_at) VALUES (?, ?, ?, ?, 'armed', ?, ?)`, symbolID, direction, level, repeat, last, now)
	if err != nil {
		return Alert{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Alert{}, err
	}
	return s.Alert(ctx, id)
}

func (s *Store) DeleteAlert(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alerts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RearmAlert(ctx context.Context, id int64) (Alert, error) {
	a, err := s.Alert(ctx, id)
	if err != nil {
		return Alert{}, err
	}
	var last *float64
	if q, err := s.Quote(ctx, a.SymbolID); err == nil {
		v := q.Price
		last = &v
	}
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET status = 'armed', triggered_at = NULL, last_price = ? WHERE id = ?`, last, id)
	if err != nil {
		return Alert{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Alert{}, ErrNotFound
	}
	return s.Alert(ctx, id)
}

// ArmedAlert is the slice of state the cross check needs.
type ArmedAlert struct {
	ID        int64
	SymbolID  int64
	Display   string
	Name      string
	Direction string
	Level     float64
	Repeat    string
	LastPrice *float64
}

func (s *Store) ArmedAlerts(ctx context.Context) ([]ArmedAlert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.symbol_id, s.display, s.name, a.direction, a.level, a.repeat, a.last_price FROM alerts a JOIN symbols s ON s.id = a.symbol_id WHERE a.status = 'armed' ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArmedAlert
	for rows.Next() {
		var a ArmedAlert
		var last sql.NullFloat64
		if err := rows.Scan(&a.ID, &a.SymbolID, &a.Display, &a.Name, &a.Direction, &a.Level, &a.Repeat, &last); err != nil {
			return nil, err
		}
		a.LastPrice = floatPtr(last)
		out = append(out, a)
	}
	return out, rows.Err()
}

// NoteContinuousFire records a cross and leaves the alert armed.
// False means it was not an armed continuous alert.
func (s *Store) NoteContinuousFire(ctx context.Context, id int64, price float64, at int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET last_price = ?, triggered_at = ? WHERE id = ? AND status = 'armed' AND repeat = 'continuous'`, price, at, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// TriggerAlert marks a cross. False means another run already triggered it.
func (s *Store) TriggerAlert(ctx context.Context, id int64, price float64, at int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET status = 'triggered', triggered_at = ?, last_price = ? WHERE id = ? AND status = 'armed'`, at, price, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) SetAlertLastPrice(ctx context.Context, id int64, price float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET last_price = ? WHERE id = ? AND status = 'armed'`, price, id)
	return err
}

type Subscription struct {
	ID       int64
	Endpoint string
	P256dh   string
	Auth     string
}

func (s *Store) SaveSubscription(ctx context.Context, endpoint, p256dh, auth string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO push_subscriptions(endpoint, p256dh, auth, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth`, endpoint, p256dh, auth, time.Now().Unix())
	return err
}

func (s *Store) DeleteSubscription(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

func (s *Store) Subscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, endpoint, p256dh, auth FROM push_subscriptions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.Endpoint, &sub.P256dh, &sub.Auth); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
