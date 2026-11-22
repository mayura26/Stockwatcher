package market

import (
	"math"
	"testing"
	"time"
)

func TestMovingAverage(t *testing.T) {
	bars := []Bar{
		{Day: "2024-01-02", Close: 1},
		{Day: "2024-01-03", Close: 2},
		{Day: "2024-01-04", Close: 3},
		{Day: "2024-01-05", Close: 4},
		{Day: "2024-01-06", Close: 5},
	}
	sma := MovingAverage("sma", 3, bars)
	if len(sma) != 3 || sma[0].Day != "2024-01-04" || sma[0].Value != 2 || sma[1].Value != 3 || sma[2].Value != 4 {
		t.Fatalf("sma %+v", sma)
	}
	ema := MovingAverage("ema", 3, bars)
	// Seed is the first SMA (2). k = 0.5, so the next values are 3 and 4.
	if len(ema) != 3 || ema[0].Value != 2 || ema[1].Value != 3 || ema[2].Value != 4 {
		t.Fatalf("ema %+v", ema)
	}
	if MovingAverage("ema", 3, bars[:2]) != nil {
		t.Fatal("period not filled")
	}
	if MovingAverage("wma", 3, bars) != nil {
		t.Fatal("unknown kind")
	}
}

func TestAlignAverageSession(t *testing.T) {
	points := []AveragePoint{
		{Day: "2024-01-03", Value: 1.5},
		{Day: "2024-01-04", Value: 2.5},
	}
	ny := time.FixedZone("EST", -5*3600)
	at := func(y int, m time.Month, d, h, min int) Candle {
		return Candle{Time: UnixTime(time.Date(y, m, d, h, min, 0, 0, ny).Unix()), Close: 1}
	}
	candles := []Candle{
		at(2024, 1, 2, 10, 0),
		at(2024, 1, 2, 10, 15),
		at(2024, 1, 3, 10, 0),
		at(2024, 1, 3, 10, 15),
		at(2024, 1, 5, 10, 0),
		{Time: DayTime("2024-01-04"), Close: 3},
	}
	got := AlignAverage(Stock, candles, points)
	if len(got) != 4 {
		t.Fatalf("len %d %+v", len(got), got)
	}
	if got[0].Time != candles[2].Time || got[1].Time != candles[3].Time || got[0].Value != 1.5 || got[1].Value != 1.5 {
		t.Fatalf("session repeat %+v", got[:2])
	}
	if got[2].Time != candles[4].Time || got[2].Value != 2.5 {
		t.Fatalf("carry %+v", got[2])
	}
	if got[3].Time.Day != "2024-01-04" || got[3].Value != 2.5 {
		t.Fatalf("daily %+v", got[3])
	}
}

func TestAlignAverageCryptoUsesUTCDay(t *testing.T) {
	// 2024-01-03 03:00 UTC is still 2024-01-02 in New York.
	ts := time.Date(2024, 1, 3, 3, 0, 0, 0, time.UTC).Unix()
	candles := []Candle{{Time: UnixTime(ts)}}
	points := []AveragePoint{{Day: "2024-01-03", Value: 9}}
	crypto := AlignAverage(Crypto, candles, points)
	if len(crypto) != 1 || crypto[0].Value != 9 {
		t.Fatalf("crypto %+v", crypto)
	}
	stock := AlignAverage(Stock, candles, points)
	if len(stock) != 0 {
		t.Fatalf("stock should wait for the New York session %+v", stock)
	}
	if math.Abs(points[0].Value-9) > 0 {
		t.Fatal("points mutated")
	}
}
