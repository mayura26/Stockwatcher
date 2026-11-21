package finnhub

import (
	"net/http"
	"testing"
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
