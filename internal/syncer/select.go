package syncer

import "stockwatcher/internal/store"

// SelectStocks picks Finnhub quote targets. Armed alerts are always included.
// When the board is larger than cap, the rest rotate from cursor so a free key
// stays under 60 calls a minute.
func SelectStocks(symbols []store.TickSymbol, cursor int64, cap int) (chosen []store.TickSymbol, next int64) {
	if cap < 1 {
		cap = 1
	}
	next = cursor
	var armed, rest []store.TickSymbol
	for _, s := range symbols {
		if s.AssetClass != "stock" {
			continue
		}
		if s.Armed {
			armed = append(armed, s)
		} else {
			rest = append(rest, s)
		}
	}
	if len(armed) >= cap {
		return armed, next
	}
	chosen = append(chosen, armed...)
	budget := cap - len(armed)
	if len(rest) == 0 || budget == 0 {
		return chosen, next
	}
	if len(rest) <= budget {
		return append(chosen, rest...), next
	}
	start := 0
	for i, s := range rest {
		if s.ID > cursor {
			start = i
			break
		}
	}
	for i := 0; i < budget; i++ {
		chosen = append(chosen, rest[(start+i)%len(rest)])
	}
	next = chosen[len(chosen)-1].ID
	return chosen, next
}
