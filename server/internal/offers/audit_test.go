package offers

import (
	"slices"
	"testing"

	"scibox/server/internal/audit"
	"scibox/server/internal/testaudit"
	"scibox/server/internal/testkit"
)

func (w *world) offerRows() []testaudit.Row {
	w.T.Helper()
	return testaudit.Rows(w.T, testkit.Pool, `org_id = $1 AND action LIKE 'offer.%'`, w.tm.OrgID)
}

func TestAuditTrailOfOffers(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	ctx := audit.WithSource(bg, "203.0.113.30")
	o, err := w.svc.Invite(ctx, w.tm.HeadA.User, InviteInput{VacancyID: w.vacancy.ID, ProfileID: profile, Message: "Приходите к нам"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Cancel(ctx, w.tm.HR.User, o.ID); err != nil {
		t.Fatal(err)
	}
	// Повторная отмена и отмена чужим ничего не записывают.
	if err := w.svc.Cancel(ctx, w.tm.HR.User, o.ID); err == nil {
		t.Error("an offer was cancelled twice")
	}
	if err := w.svc.Cancel(ctx, w.tm.Out.User, o.ID); err == nil {
		t.Error("an outsider cancelled the offer")
	}
	if _, err := w.svc.Invite(ctx, w.tm.Out.User, InviteInput{VacancyID: w.vacancy.ID, ProfileID: profile}); err == nil {
		t.Error("an outsider sent an offer")
	}

	rows := w.offerRows()
	if got := testaudit.Actions(rows); !slices.Equal(got, []string{"offer.sent", "offer.canceled"}) {
		t.Fatalf("journal = %v", got)
	}
	for i, wantActor := range []testkit.Person{w.tm.HeadA, w.tm.HR} {
		r := rows[i]
		if *r.Actor != wantActor.ID || *r.Subject != sci.ID || r.TargetKind != "offer" || *r.TargetID != o.ID || r.TargetLabel != w.vacancy.Title || *r.IP != "203.0.113.30" {
			t.Errorf("entry %d: %+v", i, r)
		}
		if len(r.Details) != 0 {
			t.Errorf("entry %d keeps details %v (the message must stay out)", i, r.Details)
		}
	}
}

func TestOffersFailWhenTheJournalIsDown(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	o := w.mustInvite(w.tm.Owner, w.vacancy.ID, profile, "")
	_, other := w.scientist("Другой", "public")
	testaudit.Break(t, testkit.Pool)

	if _, err := w.invite(w.tm.Owner, w.vacancy.ID, other, ""); err == nil {
		t.Error("the offer was sent without a journal entry")
	}
	if err := w.svc.Cancel(bg, w.tm.Owner.User, o.ID); err == nil || w.status(o.ID) != "pending" {
		t.Errorf("cancel: %v, status %s", err, w.status(o.ID))
	}
	if n := testkit.Count(t, `SELECT count(*) FROM vacancy_offers WHERE vacancy_id = $1`, w.vacancy.ID); n != 1 {
		t.Errorf("%d offers, want 1", n)
	}
	if notificationsOf(t, sci, "offer_cancelled") != 0 {
		t.Error("the person was told about a cancellation that did not happen")
	}
}
