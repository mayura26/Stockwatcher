package market

import "sort"

// AveragePoint is one daily moving-average value, keyed by the session day.
type AveragePoint struct {
	Day   string
	Value float64
}

// AverageMark is that daily value placed on a chart candle. Time matches the candle.
type AverageMark struct {
	Time  CandleTime `json:"time"`
	Value float64    `json:"value"`
}

// MovingAverage builds an EMA or SMA on daily closes. The series starts once
// `period` closes exist. kind is "ema" or "sma".
func MovingAverage(kind string, period int, bars []Bar) []AveragePoint {
	if period < 2 || len(bars) < period {
		return nil
	}
	switch kind {
	case "ema":
		return ema(period, bars)
	case "sma":
		return sma(period, bars)
	default:
		return nil
	}
}

func sma(period int, bars []Bar) []AveragePoint {
	out := make([]AveragePoint, 0, len(bars)-period+1)
	var sum float64
	for i, b := range bars {
		sum += b.Close
		if i >= period {
			sum -= bars[i-period].Close
		}
		if i >= period-1 {
			out = append(out, AveragePoint{Day: b.Day, Value: sum / float64(period)})
		}
	}
	return out
}

// ema seeds with the SMA of the first window, then applies k = 2/(period+1).
func ema(period int, bars []Bar) []AveragePoint {
	var sum float64
	for i := 0; i < period; i++ {
		sum += bars[i].Close
	}
	value := sum / float64(period)
	out := make([]AveragePoint, 0, len(bars)-period+1)
	out = append(out, AveragePoint{Day: bars[period-1].Day, Value: value})
	k := 2 / float64(period+1)
	for i := period; i < len(bars); i++ {
		value = bars[i].Close*k + value*(1-k)
		out = append(out, AveragePoint{Day: bars[i].Day, Value: value})
	}
	return out
}

// AlignAverage stamps each daily value onto the candles of that session.
// A later session with no new daily bar keeps the previous value, so a 21-day
// average still draws across today's 15-minute candles. Candles before the
// first filled value are left out. candles are in time order.
func AlignAverage(class string, candles []Candle, points []AveragePoint) []AverageMark {
	if len(candles) == 0 || len(points) == 0 {
		return nil
	}
	sorted := append([]AveragePoint(nil), points...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Day < sorted[j].Day })
	idx := 0
	var have bool
	var value float64
	out := make([]AverageMark, 0, len(candles))
	for _, c := range candles {
		day := candleDay(class, c)
		for idx < len(sorted) && sorted[idx].Day <= day {
			value = sorted[idx].Value
			have = true
			idx++
		}
		if !have || day == "" {
			continue
		}
		out = append(out, AverageMark{Time: c.Time, Value: value})
	}
	return out
}

func candleDay(class string, c Candle) string {
	if c.Time.Day != "" {
		return c.Time.Day
	}
	if class == Crypto {
		return UTCDay(c.Time.Unix)
	}
	return NYDay(c.Time.Unix)
}
