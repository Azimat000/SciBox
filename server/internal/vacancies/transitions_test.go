package vacancies

import "testing"

// Все 16 пар «из какого статуса → в какой» проверены явно.
func TestTransitionTable(t *testing.T) {
	allowed := map[[2]string]bool{
		{StatusDraft, StatusPublished}:  true,
		{StatusPublished, StatusClosed}: true,
		{StatusClosed, StatusPublished}: true,
		{StatusClosed, StatusArchived}:  true,
		{StatusArchived, StatusClosed}:  true,
	}
	for _, from := range Statuses {
		for _, to := range Statuses {
			if got, want := CanTransition(from, to), allowed[[2]string{from, to}]; got != want {
				t.Errorf("%s → %s: got %v, want %v", from, to, got, want)
			}
		}
	}
	if CanTransition("nonsense", StatusPublished) || CanTransition(StatusDraft, "nonsense") {
		t.Error("unknown statuses must never be allowed")
	}
}

func TestNextStatusesIsACopy(t *testing.T) {
	next := NextStatuses(StatusClosed)
	if len(next) != 2 {
		t.Fatalf("closed → %v", next)
	}
	next[0] = "changed"
	if NextStatuses(StatusClosed)[0] == "changed" {
		t.Error("NextStatuses must not expose the table")
	}
	if got := NextStatuses("nonsense"); len(got) != 0 {
		t.Errorf("unknown status → %v", got)
	}
}

func TestPubliclyVisible(t *testing.T) {
	want := map[string]bool{StatusDraft: false, StatusPublished: true, StatusClosed: true, StatusArchived: false}
	for st, v := range want {
		if PubliclyVisible(st) != v {
			t.Errorf("%s: want %v", st, v)
		}
	}
}
