package career

import "testing"

func TestLevel(t *testing.T) {
	tests := []struct {
		degree, title string
		want          int
	}{
		{"none", "none", 1}, {"none", "professor", 1}, {"", "", 1}, {"phd", "docent", 1},
		{"candidate", "none", 2}, {"candidate", "docent", 2}, {"candidate", "professor", 2},
		{"doctor", "none", 3}, {"doctor", "docent", 3},
		{"doctor", "professor", 4},
	}
	for _, tc := range tests {
		if got := Level(tc.degree, tc.title); got != tc.want {
			t.Errorf("Level(%q, %q) = %d, ожидали %d", tc.degree, tc.title, got, tc.want)
		}
	}
}
