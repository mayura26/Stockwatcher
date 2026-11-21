package finnhub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseQuote(t *testing.T) {
	q, err := ParseQuote([]byte(`{"c":189.2,"d":0.8,"dp":0.42,"h":190,"l":188,"o":188.5,"pc":188.4,"t":1700000000}`))
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 189.2 || q.PrevClose != 188.4 || q.DayPct == nil || *q.DayPct != 0.42 {
		t.Fatalf("%+v", q)
	}
	if _, err := ParseQuote([]byte(`{"c":0,"pc":1,"dp":0,"t":1}`)); err == nil {
		t.Fatal("expected empty quote error")
	}
}

func TestParseSearchFiltersUSCommonStock(t *testing.T) {
	raw := []byte(`{"count":3,"result":[
		{"description":"APPLE INC","displaySymbol":"AAPL","symbol":"AAPL","type":"Common Stock"},
		{"description":"APPLE","displaySymbol":"APC.DE","symbol":"APC.DE","type":"Common Stock"},
		{"description":"ETF","displaySymbol":"AAPLX","symbol":"AAPLX","type":"ETP"}
	]}`)
	hits, err := ParseSearch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ProviderSymbol != "AAPL" || hits[0].Name != "APPLE INC" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseCandles(t *testing.T) {
	bars, err := ParseCandles([]byte(`{"c":[10,11],"h":[12,12],"l":[9,10],"o":[9.5,10.5],"s":"ok","t":[1700000000,1700086400],"v":[100,110]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 || bars[0].Close != 10 || bars[1].Volume != 110 {
		t.Fatalf("%+v", bars)
	}
}

func TestParseMetric(t *testing.T) {
	cap, pe, err := ParseMetric([]byte(`{"metric":{"marketCapitalization":3400.5,"peTTM":32.41,"peBasicExclExtraTTM":10},"symbol":"AAPL"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cap == nil || *cap != 3400.5*million || pe == nil || *pe != 32.41 {
		t.Fatalf("cap %v pe %v", cap, pe)
	}
	cap, pe, err = ParseMetric([]byte(`{"metric":{"marketCapitalization":0,"peTTM":-1}}`))
	if err != nil || cap != nil || pe != nil {
		t.Fatalf("non-positive cap %v pe %v err %v", cap, pe, err)
	}
	if _, _, err := ParseMetric([]byte(`{"error":"no access"}`)); err == nil {
		t.Fatal("expected api error")
	}
}

func TestParseEarningsCalendar(t *testing.T) {
	raw := []byte(`{"earningsCalendar":[
		{"date":"2025-12-01","hour":"amc","symbol":"AAPL"},
		{"date":"2026-04-30","hour":"bmo","symbol":"AAPL"},
		{"date":"2026-01-29","hour":"amc","symbol":"AAPL"},
		{"date":"2026-02-02","hour":"dmh","symbol":"AAPL"}
	]}`)
	date, hour, err := ParseEarningsCalendar(raw, "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-01-29" || hour != "amc" {
		t.Fatalf("%s %s", date, hour)
	}
	date, hour, err = ParseEarningsCalendar([]byte(`{"earningsCalendar":[{"date":"2026-02-02","hour":"dmh"}]}`), "2026-01-01")
	if err != nil || date != "2026-02-02" || hour != "" {
		t.Fatalf("%s %q %v", date, hour, err)
	}
	date, hour, err = ParseEarningsCalendar([]byte(`{"earningsCalendar":[]}`), "2026-01-01")
	if err != nil || date != "" || hour != "" {
		t.Fatalf("empty %s %q %v", date, hour, err)
	}
}

func TestParseReportedTTM(t *testing.T) {
	raw := []byte(`{"data":[
		{"endDate":"2024-09-30","filedDate":"2024-11-01","report":{"ic":[
			{"concept":"us-gaap_RevenueFromContractWithCustomerExcludingAssessedTax","unit":"usd","value":100},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":100}
		]}},
		{"endDate":"2024-12-30","filedDate":"2025-02-01","report":{"ic":[
			{"concept":"us-gaap_RevenuesSomething","unit":"usd","value":999},
			{"concept":"http://fasb.org/us-gaap/2024#RevenueFromContractWithCustomerExcludingAssessedTax","unit":"shares","value":1},
			{"concept":"http://fasb.org/us-gaap/2024#RevenueFromContractWithCustomerExcludingAssessedTax","unit":"usd","value":20},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":2}
		]}},
		{"endDate":"2025-03-30","filedDate":"2025-05-01","report":{"ic":[
			{"concept":"us-gaap_RevenueFromContractWithCustomerExcludingAssessedTax","unit":"usd","value":10},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":1}
		]}},
		{"endDate":"2025-03-30","filedDate":"2025-05-20","report":{"ic":[
			{"concept":"us-gaap_RevenueFromContractWithCustomerExcludingAssessedTax","unit":"usd","value":12}
		]}},
		{"endDate":"2025-06-30","filedDate":"2025-08-01","report":{"ic":[
			{"concept":"Revenues","unit":"usd","value":30},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":3}
		]}},
		{"endDate":"2025-09-30","filedDate":"2025-11-01","report":{"ic":[
			{"concept":"SalesRevenueNet","unit":"usd","value":40},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":-4}
		]}}
	]}`)
	rev, earn, err := ParseReportedTTM(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rev == nil || *rev != 102 {
		t.Fatalf("revenue %v", rev)
	}
	if earn != nil {
		t.Fatalf("earnings should be hidden when a recent quarter has no net income, got %v", *earn)
	}

	short := []byte(`{"data":[
		{"endDate":"2025-03-30","filedDate":"2025-05-01","report":{"ic":[
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":1}
		]}},
		{"endDate":"2025-06-30","filedDate":"2025-08-01","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":2},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":1}
		]}},
		{"endDate":"2025-09-30","filedDate":"2025-11-01","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":3},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":1}
		]}}
	]}`)
	rev, earn, err = ParseReportedTTM(short, nil)
	if err != nil || rev != nil || earn != nil {
		t.Fatalf("partial ttm rev %v earn %v err %v", rev, earn, err)
	}
}

func TestFactsRequests(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &Client{Key: "k", Base: srv.URL, HTTP: srv.Client()}
	now := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	if _, _, err := c.Metric(t.Context(), "AAPL"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.NextEarnings(t.Context(), "AAPL", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReportedTTM(t.Context(), "AAPL", now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(paths[0], "/stock/metric?") || !strings.Contains(paths[0], "metric=all") || !strings.Contains(paths[0], "symbol=AAPL") {
		t.Fatalf("metric %s", paths[0])
	}
	if !strings.Contains(paths[1], "/calendar/earnings?") || !strings.Contains(paths[1], "from=2026-01-15") || !strings.Contains(paths[1], "to=2026-04-15") {
		t.Fatalf("calendar %s", paths[1])
	}
	if len(paths) != 4 {
		t.Fatalf("%v", paths)
	}
	if !strings.Contains(paths[2], "/stock/financials-reported?") || !strings.Contains(paths[2], "freq=quarterly") || !strings.Contains(paths[2], "from=2024-01-15") {
		t.Fatalf("quarterly %s", paths[2])
	}
	if !strings.Contains(paths[3], "freq=annual") || !strings.Contains(paths[3], "from=2024-01-15") {
		t.Fatalf("annual %s", paths[3])
	}
}

func TestParseReportedTTMSplitsYearToDate(t *testing.T) {
	quarterly := []byte(`{"data":[
		{"startDate":"2025-01-27 00:00:00","endDate":"2025-04-27 00:00:00","filedDate":"2025-05-28 00:00:00","form":"10-Q","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":10},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":1}
		]}},
		{"startDate":"2025-01-27 00:00:00","endDate":"2025-07-27 00:00:00","filedDate":"2025-08-27 00:00:00","form":"10-Q","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":25},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":3}
		]}},
		{"startDate":"2025-01-27 00:00:00","endDate":"2025-10-26 00:00:00","filedDate":"2025-11-19 00:00:00","form":"10-Q","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":40},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":6}
		]}}
	]}`)
	annual := []byte(`{"data":[
		{"startDate":"2025-01-27 00:00:00","endDate":"2026-01-25 00:00:00","filedDate":"2026-02-26 00:00:00","form":"10-K","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":60},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":10}
		]}},
		{"startDate":"2024-01-29 00:00:00","endDate":"2025-01-26 00:00:00","filedDate":"2025-02-26 00:00:00","form":"10-K","report":{"ic":[
			{"concept":"us-gaap_Revenues","unit":"usd","value":100},
			{"concept":"us-gaap_NetIncomeLoss","unit":"usd","value":40}
		]}}
	]}`)
	rev, earn, err := ParseReportedTTM(quarterly, annual)
	if err != nil {
		t.Fatal(err)
	}
	if rev == nil || *rev != 60 || earn == nil || *earn != 10 {
		t.Fatalf("rev %v earn %v", rev, earn)
	}
	rev, earn, err = ParseReportedTTM(quarterly, nil)
	if err != nil || rev != nil || earn != nil {
		t.Fatalf("missing fourth quarter rev %v earn %v err %v", rev, earn, err)
	}
}

func TestCandleAllowed(t *testing.T) {
	if CandleAllowed(http.StatusForbidden, []byte(`{"error":"nope"}`)) {
		t.Fatal("403 should deny")
	}
	if !CandleAllowed(http.StatusOK, []byte(`{"s":"no_data"}`)) {
		t.Fatal("no_data is still authorized")
	}
	if CandleAllowed(http.StatusOK, []byte(`{"s":"error","error":"paid"}`)) {
		t.Fatal("error body should deny")
	}
}
