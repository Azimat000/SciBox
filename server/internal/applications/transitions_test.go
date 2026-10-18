package applications

import (
	"slices"
	"testing"
)

// Все 25 пар «из → в» для статусов отклика проверены явно, плюс неизвестные статусы и бывший «приглашён» (D-256).
func TestStaffMovesTable(t *testing.T) {
	allowed := map[[2]string]bool{
		{StatusSent, StatusViewed}: true, {StatusSent, StatusRejected}: true,
		{StatusViewed, StatusRejected}: true, {StatusViewed, StatusAccepted}: true,
	}
	for _, from := range append(slices.Clone(Statuses), "", "unknown", "invited") {
		for _, to := range append(slices.Clone(Statuses), "", "unknown", "invited") {
			if got, want := CanStaffMove(from, to), allowed[[2]string{from, to}]; got != want {
				t.Errorf("CanStaffMove(%q → %q) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestDecisions(t *testing.T) {
	want := map[string][]string{
		StatusSent:      {StatusRejected},
		StatusViewed:    {StatusAccepted, StatusRejected},
		"invited":       {},
		StatusRejected:  {},
		StatusAccepted:  {},
		StatusWithdrawn: {},
		"unknown":       {},
	}
	for from, decisions := range want {
		if got := DecisionsFrom(from); !slices.Equal(got, decisions) {
			t.Errorf("DecisionsFrom(%q) = %v, want %v", from, got, decisions)
		}
		for _, to := range append(slices.Clone(Statuses), "") {
			if got, want := CanDecide(from, to), slices.Contains(decisions, to); got != want {
				t.Errorf("CanDecide(%q → %q) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestFromStatuses(t *testing.T) {
	cases := map[string][]string{
		StatusViewed:    {StatusSent},
		StatusRejected:  {StatusSent, StatusViewed},
		StatusAccepted:  {StatusViewed},
		StatusWithdrawn: nil,
		StatusSent:      nil,
	}
	for to, want := range cases {
		if got := fromStatuses(to); !slices.Equal(got, want) {
			t.Errorf("fromStatuses(%q) = %v, want %v", to, got, want)
		}
	}
}
