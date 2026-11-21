package alerts

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
