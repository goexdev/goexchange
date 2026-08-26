package risk

import "testing"

func TestActionForScore(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{0, ActionAllow},
		{10, ActionAllow},
		{30, ActionAllow},
		{31, ActionHold},
		{50, ActionHold},
		{60, ActionHold},
		{61, ActionBlock},
		{100, ActionBlock},
	}
	for _, tc := range tests {
		if got := ActionForScore(tc.score); got != tc.want {
			t.Errorf("ActionForScore(%d) = %s, want %s", tc.score, got, tc.want)
		}
	}
}
