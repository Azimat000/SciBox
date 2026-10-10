package jobs

import (
	"testing"
	"time"
)

func TestJitterStaysWithinTenPercentAndLeavesTinyPausesAlone(t *testing.T) {
	const d = time.Minute
	for range 500 {
		if got := jitter(d); got < d-d/10 || got > d+d/10 {
			t.Fatalf("jitter(%v) = %v, want within ±10%%", d, got)
		}
	}
	if got := jitter(3); got != 3 {
		t.Errorf("jitter(3ns) = %v, want unchanged", got)
	}
	if got := jitter(0); got != 0 {
		t.Errorf("jitter(0) = %v, want 0", got)
	}
}

func TestHolderNamesDifferBetweenCopies(t *testing.T) {
	if a, b := newHolder(), newHolder(); a == b || a == "" {
		t.Errorf("holders %q and %q must be unique and non-empty", a, b)
	}
}
