package alerts

import "testing"

func TestCrossed(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		level     float64
		prev      float64
		last      float64
		want      bool
	}{
		{"above touches level", "above", 10, 9, 10, true},
		{"above through level", "above", 10, 9, 11, true},
		{"above already above", "above", 10, 10, 11, false},
		{"above still under", "above", 10, 9, 9.5, false},
		{"below touches level", "below", 10, 11, 10, true},
		{"below through level", "below", 10, 11, 9, true},
		{"below already at level", "below", 10, 10, 9, false},
		{"below still over", "below", 10, 12, 10.5, false},
		{"unknown direction", "sideways", 10, 1, 20, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Crossed(tt.direction, tt.level, tt.prev, tt.last); got != tt.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestCrossPhrase(t *testing.T) {
	if CrossPhrase("above") != "up" || CrossPhrase("below") != "down" || CrossPhrase("sideways") != "" {
		t.Fatalf("up %q down %q other %q", CrossPhrase("above"), CrossPhrase("below"), CrossPhrase("sideways"))
	}
}

func TestEvaluateFirstObservationSilent(t *testing.T) {
	fire, next := Evaluate("above", 10, nil, 100)
	if fire || next != 100 {
		t.Fatalf("fire %v next %v", fire, next)
	}
	prev := 9.0
	fire, next = Evaluate("above", 10, &prev, 10)
	if !fire || next != 10 {
		t.Fatalf("fire %v next %v", fire, next)
	}
}
