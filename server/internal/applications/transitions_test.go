package applications

import (
	"slices"
	"testing"
)

// Все 36 пар «из → в» для статусов отклика проверены явно, плюс неизвестные статусы.
func TestStaffMovesTable(t *testing.T) {
	allowed := map[[2]string]bool{
		{StatusSent, StatusViewed}: true, {StatusSent, StatusInvited}: true, {StatusSent, StatusRejected}: true,
		{StatusViewed, StatusInvited}: true, {StatusViewed, StatusRejected}: true, {StatusViewed, StatusAccepted}: true,
		{StatusInvited, StatusRejected}: true, {StatusInvited, StatusAccepted}: true,
	}
	for _, from := range append(slices.Clone(Statuses), "", "unknown") {
		for _, to := range append(slices.Clone(Statuses), "", "unknown") {
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
		StatusInvited:   {StatusAccepted, StatusRejected},
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

func TestCanInvite(t *testing.T) {
	want := map[string]bool{StatusSent: true, StatusViewed: true, StatusInvited: true, StatusRejected: false, StatusAccepted: false, StatusWithdrawn: false, "": false}
	for from, w := range want {
		if got := CanInvite(from); got != w {
			t.Errorf("CanInvite(%q) = %v, want %v", from, got, w)
		}
	}
}

func TestFromStatuses(t *testing.T) {
	cases := map[string][]string{
		StatusViewed:    {StatusSent},
		StatusInvited:   {StatusSent, StatusViewed},
		StatusRejected:  {StatusSent, StatusViewed, StatusInvited},
		StatusAccepted:  {StatusViewed, StatusInvited},
		StatusWithdrawn: nil,
		StatusSent:      nil,
	}
	for to, want := range cases {
		if got := fromStatuses(to); !slices.Equal(got, want) {
			t.Errorf("fromStatuses(%q) = %v, want %v", to, got, want)
		}
	}
}

// Каждая пара «вид приглашения × ответ» проверена явно.
func TestAnswerTarget(t *testing.T) {
	kinds := append(slices.Clone(InvitationKinds), "", "unknown")
	actions := []string{AnswerConfirm, AnswerPropose, AnswerReply, "", "unknown"}
	want := map[[2]string]string{
		{InvInterview, AnswerConfirm}: InvConfirmed,
		{InvInterview, AnswerPropose}: InvProposed,
		{InvRequest, AnswerReply}:     InvAnswered,
	}
	for _, k := range kinds {
		for _, a := range actions {
			got, ok := AnswerTarget(k, a)
			w, wok := want[[2]string{k, a}]
			if got != w || ok != wok {
				t.Errorf("AnswerTarget(%q, %q) = %q, %v; want %q, %v", k, a, got, ok, w, wok)
			}
		}
	}
}

func TestInvitationStateRules(t *testing.T) {
	for _, st := range append(slices.Clone(InvitationStatuses), "", "unknown") {
		wantCancel := st == InvPending || st == InvProposed || st == InvConfirmed
		if got := CanCancelInvitation(st); got != wantCancel {
			t.Errorf("CanCancelInvitation(%q) = %v", st, got)
		}
		if got := CanAnswerInvitation(st); got != (st == InvPending) {
			t.Errorf("CanAnswerInvitation(%q) = %v", st, got)
		}
		for _, k := range append(slices.Clone(InvitationKinds), "unknown") {
			if got, want := CanAcceptProposal(k, st), k == InvInterview && st == InvProposed; got != want {
				t.Errorf("CanAcceptProposal(%q, %q) = %v", k, st, got)
			}
		}
	}
	for kind, want := range map[string]string{InvInterview: InvPending, InvRequest: InvPending, InvContacts: InvShared, "": "", "unknown": ""} {
		if got := InitialInvitationStatus(kind); got != want {
			t.Errorf("InitialInvitationStatus(%q) = %q, want %q", kind, got, want)
		}
	}
}
