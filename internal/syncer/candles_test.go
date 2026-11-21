package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockwatcher/internal/market"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/store"
)

func TestIntradayCacheSkipsFreshAndTailsStale(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"chart":{"result":[{"timestamp":[1700000000],"indicators":{"quote":[{"open":[9],"high":[11],"low":[8],"close":[10]}]}}],"error":null}}`))
	}))
	defer srv.Close()

	st, gid := openWatch(t)
	defer st.Close()
	ctx := context.Background()
	sym, _, err := st.AddToGroup(ctx, gid, store.NewSymbol{AssetClass: market.Stock, Display: "AAPL", ProviderSymbol: "AAPL", Name: "Apple"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	sy := New(st, nil, &yahoo.Client{Base: srv.URL}, nil, nil)
	sy.Now = func() time.Time { return now }

	if _, err := sy.SpanCandles(ctx, sym.ID, "D"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "range=5d") || !strings.Contains(calls[0], "interval=15m") {
		t.Fatalf("full fetch %#v", calls)
	}

	start := market.CacheStart(market.Interval15m, now)
	fresh := market.BucketOpen(now, market.Interval15m)
	if err := st.UpsertIntraday(ctx, sym.ID, market.Interval15m, []market.IntradayBar{
		{OpenUnix: start, Open: 1, High: 1, Low: 1, Close: 1},
		{OpenUnix: fresh, Open: 2, High: 2, Low: 2, Close: 2},
	}); err != nil {
		t.Fatal(err)
	}
	calls = nil
	if _, err := sy.SpanCandles(ctx, sym.ID, "D"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("fresh cache called yahoo %#v", calls)
	}

	stale := fresh - market.IntervalStep(market.Interval15m)
	if err := st.PruneIntraday(ctx, sym.ID, market.Interval15m, 1<<62); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertIntraday(ctx, sym.ID, market.Interval15m, []market.IntradayBar{
		{OpenUnix: start, Open: 1, High: 1, Low: 1, Close: 1},
		{OpenUnix: stale, Open: 3, High: 3, Low: 3, Close: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sy.SpanCandles(ctx, sym.ID, "D"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "period1=") || strings.Contains(calls[0], "range=") {
		t.Fatalf("tail fetch %#v", calls)
	}
	want := fmt.Sprintf("period1=%d", stale)
	if !strings.Contains(calls[0], want) {
		t.Fatalf("tail from %s, query %s", want, calls[0])
	}
}
