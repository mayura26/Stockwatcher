package binance

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestParseTickers(t *testing.T) {
	raw := []byte(`[{"symbol":"BTCUSDT","lastPrice":"59075.28000000","prevClosePrice":"62539.27000000","priceChangePercent":"-5.539","closeTime":1795223338013}]`)
	got, err := ParseTickers(raw)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := got["BTCUSDT"]
	if !ok || math.Abs(q.Price-59075.28) > 1e-6 || q.DayPct == nil || *q.DayPct != -5.539 {
		t.Fatalf("%+v", q)
	}
	if _, err := ParseTickers([]byte(`{"code":-1121,"msg":"Invalid symbol."}`)); err == nil {
		t.Fatal("expected api error")
	}
}

func TestParseKlines(t *testing.T) {
	raw := []byte(`[[1795132800000,"61986.73","62639.52","57778.35","58951.51","38125.92",1795219199999,"1",1,"1","1","0"]]`)
	bars, err := ParseKlines(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 1 || bars[0].Close != 58951.51 {
		t.Fatalf("%+v", bars)
	}
	want := time.UnixMilli(1795132800000).UTC().Format("2006-01-02")
	if bars[0].Day != want {
		t.Fatalf("day %s want %s", bars[0].Day, want)
	}
}

func TestParseIntraday(t *testing.T) {
	raw := []byte(`[[1795132800000,"100","110","90","105","1",1795133099999,"1",1,"1","1","0"]]`)
	pts, err := ParseIntraday(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].Price != 105 || !pts[0].Time.Equal(time.UnixMilli(1795133099999)) {
		t.Fatalf("%+v", pts)
	}
	if _, err := ParseIntraday([]byte(`{"code":-1121,"msg":"Invalid symbol."}`)); err == nil {
		t.Fatal("expected api error")
	}
}

func TestParseExchangeInfoAndSearch(t *testing.T) {
	raw := []byte(`{"symbols":[
		{"symbol":"BTCUSDT","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT","isSpotTradingAllowed":true},
		{"symbol":"ETHUSDT","status":"TRADING","baseAsset":"ETH","quoteAsset":"USDT","isSpotTradingAllowed":true},
		{"symbol":"BTCBUSD","status":"TRADING","baseAsset":"BTC","quoteAsset":"BUSD","isSpotTradingAllowed":true},
		{"symbol":"SOLUSDT","status":"BREAK","baseAsset":"SOL","quoteAsset":"USDT","isSpotTradingAllowed":true}
	]}`)
	pairs, err := ParseExchangeInfo(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 {
		t.Fatalf("%+v", pairs)
	}
	c := &Client{pairs: pairs, pairsAt: time.Now()}
	hits, err := c.Search(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Display != "BTC" || hits[1].Display != "ETH" {
		t.Fatalf("%+v", hits)
	}
	hits, err = c.Search(context.Background(), "et")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ProviderSymbol != "ETHUSDT" {
		t.Fatalf("%+v", hits)
	}
}
