package alerts

import (
	"errors"
	"math"
)

var (
	ErrBadPercent = errors.New("bad percent")
	ErrBadPrice   = errors.New("bad price")
	ErrTight      = errors.New("tight bracket")
)

// BracketLevels is the up and down prices for a percent band around price.
// Percent is a number such as 10 for 10%. Each level uses the same decimal
// places as a chart price of that size.
func BracketLevels(price, percent float64) (up, down float64, err error) {
	if !(percent > 0) || percent >= 100 || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0, 0, ErrBadPercent
	}
	if !(price > 0) || math.IsNaN(price) || math.IsInf(price, 0) {
		return 0, 0, ErrBadPrice
	}
	up = roundLevel(price * (1 + percent/100))
	down = roundLevel(price * (1 - percent/100))
	if !(down > 0) || !(up > down) {
		return 0, 0, ErrTight
	}
	return up, down, nil
}

func roundLevel(v float64) float64 {
	abs := math.Abs(v)
	digits := 2
	if abs > 0 && abs < 1 {
		if abs < 0.01 {
			digits = 6
		} else {
			digits = 4
		}
	}
	scale := math.Pow(10, float64(digits))
	return math.Round(v*scale) / scale
}

// CrossPhrase is the push wording for a stored direction.
func CrossPhrase(direction string) string {
	switch direction {
	case "above":
		return "up"
	case "below":
		return "down"
	default:
		return ""
	}
}

// Crossed reports a price cross. Touching the level counts.
// Already sitting on the level does not count: prev must be strictly on the other side.
func Crossed(direction string, level, prev, last float64) bool {
	switch direction {
	case "above":
		return prev < level && last >= level
	case "below":
		return prev > level && last <= level
	default:
		return false
	}
}

// Evaluate applies cross detection. A nil previous price is the first observation
// and never fires. The returned price is what the alert should store as last_price.
func Evaluate(direction string, level float64, prev *float64, last float64) (fire bool, next float64) {
	if prev == nil {
		return false, last
	}
	return Crossed(direction, level, *prev, last), last
}
