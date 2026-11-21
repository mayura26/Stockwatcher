package finnhub

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"stockwatcher/internal/market"
)

const million = 1_000_000

// revenueConcepts is the first GAAP line that counts as revenue, in order.
var revenueConcepts = []string{
	"RevenueFromContractWithCustomerExcludingAssessedTax",
	"Revenues",
	"SalesRevenueNet",
}

// earningsConcepts is net income. A filer that uses another name omits Earn.
var earningsConcepts = []string{
	"NetIncomeLoss",
}

// Metric loads market cap (dollars) and TTM P/E. A missing or non-positive
// figure comes back nil. Market cap on this route is reported in millions.
func (c *Client) Metric(ctx context.Context, symbol string) (marketCap, pe *float64, err error) {
	if !c.Enabled() {
		return nil, nil, ErrNoKey
	}
	body, err := c.get(ctx, "/stock/metric?symbol="+url.QueryEscape(symbol)+"&metric=all")
	if err != nil {
		return nil, nil, err
	}
	return ParseMetric(body)
}

// NextEarnings is the earliest release from today through 90 days.
// date is YYYY-MM-DD. hour is "bmo", "amc", or empty.
func (c *Client) NextEarnings(ctx context.Context, symbol string, now time.Time) (date, hour string, err error) {
	if !c.Enabled() {
		return "", "", ErrNoKey
	}
	from, to := earningsWindow(now)
	path := "/calendar/earnings?symbol=" + url.QueryEscape(symbol) + "&from=" + from + "&to=" + to
	body, err := c.get(ctx, path)
	if err != nil {
		return "", "", err
	}
	return ParseEarningsCalendar(body, from)
}

// ReportedTTM sums the last four reported quarters. A 10-Q income statement is
// year-to-date, so the annual filing is loaded too and the fourth quarter is
// the year minus the prior year-to-date. Fewer than four contiguous quarters
// returns nil for that concept.
func (c *Client) ReportedTTM(ctx context.Context, symbol string, now time.Time) (revenue, earnings *float64, err error) {
	if !c.Enabled() {
		return nil, nil, ErrNoKey
	}
	from := reportedFrom(now)
	base := "/stock/financials-reported?symbol=" + url.QueryEscape(symbol) + "&from=" + url.QueryEscape(from) + "&freq="
	quarterly, err := c.get(ctx, base+"quarterly")
	if err != nil {
		return nil, nil, err
	}
	annual, err := c.get(ctx, base+"annual")
	if err != nil {
		return nil, nil, err
	}
	return ParseReportedTTM(quarterly, annual)
}

func earningsWindow(now time.Time) (from, to string) {
	day := market.NYDay(now.Unix())
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		t = now.UTC()
		day = t.Format("2006-01-02")
	}
	return day, t.AddDate(0, 0, 90).Format("2006-01-02")
}

func reportedFrom(now time.Time) string {
	day := market.NYDay(now.Unix())
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		t = now.UTC()
	}
	// Two years leaves the 10-K and the year-to-date quarter before it in range.
	return t.AddDate(-2, 0, 0).Format("2006-01-02")
}

// ParseMetric decodes /stock/metric. Market cap is converted from millions to dollars.
func ParseMetric(data []byte) (marketCap, pe *float64, err error) {
	if err := apiError(data); err != nil {
		return nil, nil, err
	}
	var raw struct {
		Metric struct {
			MarketCapitalization *float64 `json:"marketCapitalization"`
			PeTTM                *float64 `json:"peTTM"`
		} `json:"metric"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, err
	}
	if v := positive(raw.Metric.MarketCapitalization); v != nil {
		scaled := *v * million
		marketCap = &scaled
	}
	pe = positive(raw.Metric.PeTTM)
	return marketCap, pe, nil
}

// ParseEarningsCalendar keeps the earliest release on or after today.
func ParseEarningsCalendar(data []byte, today string) (date, hour string, err error) {
	if err := apiError(data); err != nil {
		return "", "", err
	}
	var raw struct {
		EarningsCalendar []struct {
			Date string `json:"date"`
			Hour string `json:"hour"`
		} `json:"earningsCalendar"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", "", err
	}
	for _, ev := range raw.EarningsCalendar {
		if len(ev.Date) != 10 || (today != "" && ev.Date < today) {
			continue
		}
		if date == "" || ev.Date < date {
			date = ev.Date
			hour = earningsHour(ev.Hour)
		}
	}
	return date, hour, nil
}

func earningsHour(hour string) string {
	switch strings.ToLower(strings.TrimSpace(hour)) {
	case "bmo":
		return "bmo"
	case "amc":
		return "amc"
	default:
		return ""
	}
}

type reportedLine struct {
	Concept string   `json:"concept"`
	Unit    string   `json:"unit"`
	Value   *float64 `json:"value"`
}

type reportedFiling struct {
	start, end, filed string
	revenue, earnings *float64
}

type reportedQuarter struct {
	end               string
	revenue, earnings *float64
}

// ParseReportedTTM reads quarterly and annual as-reported filings and sums the
// newest four contiguous quarters. quarterly and annual are the two freq
// responses. A nil annual body is treated as no annual filings.
func ParseReportedTTM(quarterly, annual []byte) (revenue, earnings *float64, err error) {
	rows, err := parseReported(quarterly)
	if err != nil {
		return nil, nil, err
	}
	if len(annual) > 0 {
		more, err := parseReported(annual)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, more...)
	}
	quarters := isolateQuarters(rows)
	return sumQuarters(quarters, true), sumQuarters(quarters, false), nil
}

func parseReported(data []byte) ([]reportedFiling, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if err := apiError(data); err != nil {
		return nil, err
	}
	var raw struct {
		Data []struct {
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
			FiledDate string `json:"filedDate"`
			Report    struct {
				IC []reportedLine `json:"ic"`
			} `json:"report"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	byEnd := map[string]reportedFiling{}
	for _, row := range raw.Data {
		end := filingDay(row.EndDate)
		if end == "" {
			continue
		}
		filed := filingDay(row.FiledDate)
		cur, ok := byEnd[end]
		if ok && filed < cur.filed {
			continue
		}
		byEnd[end] = reportedFiling{
			start:    filingDay(row.StartDate),
			end:      end,
			filed:    filed,
			revenue:  lineValue(row.Report.IC, revenueConcepts),
			earnings: lineValue(row.Report.IC, earningsConcepts),
		}
	}
	out := make([]reportedFiling, 0, len(byEnd))
	for _, row := range byEnd {
		out = append(out, row)
	}
	return out, nil
}

// isolateQuarters turns year-to-date statements into single quarters.
// Filings that share a start date are one fiscal year: the first quarter is
// kept, and each later statement is the increase since the previous one.
// A lone annual total is left out. A filing with no start date is already a quarter.
func isolateQuarters(rows []reportedFiling) []reportedQuarter {
	byEnd := map[string]reportedFiling{}
	for _, row := range rows {
		cur, ok := byEnd[row.end]
		if ok && row.filed < cur.filed {
			continue
		}
		byEnd[row.end] = row
	}
	byStart := map[string][]reportedFiling{}
	var plain []reportedFiling
	for _, row := range byEnd {
		if row.start == "" {
			plain = append(plain, row)
			continue
		}
		byStart[row.start] = append(byStart[row.start], row)
	}
	var out []reportedQuarter
	for _, row := range plain {
		out = append(out, reportedQuarter{end: row.end, revenue: row.revenue, earnings: row.earnings})
	}
	for _, group := range byStart {
		sort.Slice(group, func(i, j int) bool { return group[i].end < group[j].end })
		for i, row := range group {
			if i == 0 {
				if quarterSpan(row.start, row.end) {
					out = append(out, reportedQuarter{end: row.end, revenue: row.revenue, earnings: row.earnings})
				}
				continue
			}
			prev := group[i-1]
			out = append(out, reportedQuarter{
				end:      row.end,
				revenue:  subtract(row.revenue, prev.revenue),
				earnings: subtract(row.earnings, prev.earnings),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].end > out[j].end })
	return out
}

func sumQuarters(quarters []reportedQuarter, revenue bool) *float64 {
	if len(quarters) < 4 {
		return nil
	}
	newest := quarters[:4]
	for i := 0; i < 3; i++ {
		if !withinQuarter(newest[i].end, newest[i+1].end) {
			return nil
		}
	}
	var sum float64
	for _, row := range newest {
		v := row.earnings
		if revenue {
			v = row.revenue
		}
		if v == nil {
			return nil
		}
		sum += *v
	}
	return &sum
}

func subtract(cur, prev *float64) *float64 {
	if !finite(cur) || !finite(prev) {
		return nil
	}
	v := *cur - *prev
	return &v
}

func filingDay(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return ""
	}
	return s[:10]
}

func quarterSpan(start, end string) bool {
	days := dayGap(start, end)
	return days > 0 && days <= 120
}

func withinQuarter(later, earlier string) bool {
	days := dayGap(earlier, later)
	return days > 0 && days <= 120
}

func dayGap(a, b string) int {
	ta, ea := time.Parse("2006-01-02", a)
	tb, eb := time.Parse("2006-01-02", b)
	if ea != nil || eb != nil {
		return 0
	}
	d := tb.Sub(ta)
	if d < 0 {
		d = -d
	}
	return int(d.Hours() / 24)
}

func lineValue(items []reportedLine, names []string) *float64 {
	for _, name := range names {
		var fallback *float64
		for _, item := range items {
			if !conceptMatch(item.Concept, name) || !finite(item.Value) {
				continue
			}
			unit := strings.ToLower(strings.TrimSpace(item.Unit))
			if unit == "usd" {
				v := *item.Value
				return &v
			}
			if unit == "" && fallback == nil {
				v := *item.Value
				fallback = &v
			}
		}
		if fallback != nil {
			return fallback
		}
	}
	return nil
}

func conceptMatch(concept, name string) bool {
	concept = strings.TrimSpace(concept)
	if concept == name {
		return true
	}
	for _, sep := range []string{"_", "#", "/"} {
		if strings.HasSuffix(concept, sep+name) {
			return true
		}
	}
	return false
}

func positive(v *float64) *float64 {
	if !finite(v) || *v <= 0 {
		return nil
	}
	return v
}

func finite(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0)
}

func apiError(data []byte) error {
	var raw struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Error != "" {
		return errors.New(raw.Error)
	}
	return nil
}
