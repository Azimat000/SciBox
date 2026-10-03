package applications

import (
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) string { return fixedNow.Add(d).In(moscow).Format(time.RFC3339) }

func TestParseMoment(t *testing.T) {
	cases := []struct {
		name, raw string
		want      string // сообщение об ошибке; пусто — разобрано
	}{
		{"empty", "", "needed"},
		{"blanks", "   ", "needed"},
		{"garbage", "завтра", msgTimeBad},
		{"date only", "2026-10-05", msgTimeBad},
		{"now", at(0), msgTimePast},
		{"past", at(-time.Hour), msgTimePast},
		{"one minute ahead", at(time.Minute), ""},
		{"a year ahead", at(365 * 24 * time.Hour), ""},
		{"too far", at(367 * 24 * time.Hour), msgTimeFar},
	}
	for _, c := range cases {
		got, msg := parseMoment(c.raw, fixedNow, "needed")
		if msg != c.want {
			t.Errorf("%s: msg = %q, want %q", c.name, msg, c.want)
		}
		if msg == "" && (got.Location() != time.UTC || got.Second() != 0) {
			t.Errorf("%s: moment must be UTC and cut to minutes: %v", c.name, got)
		}
	}
	// Секунды отбрасываются, смещение учитывается.
	got, _ := parseMoment("2026-10-10T14:30:45+03:00", fixedNow, "")
	if want := time.Date(2026, 10, 10, 11, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("parsed = %v, want %v", got, want)
	}
}

func TestCleanPhone(t *testing.T) {
	good := map[string]string{"+7 (913) 123-45-67": "+7 (913) 123-45-67", "  89131234567 ": "89131234567", "12345": "12345", "+7  913   1234567": "+7 913 1234567"}
	for in, want := range good {
		if got, ok := cleanPhone(in); !ok || got != want {
			t.Errorf("cleanPhone(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"1234", "7-9-1", "abc12345", "8 913 123 45 67 доб. 5", "+1234567890123456", "1+2345678", "+7<913>1234567", "+7 913 1234567" + strings.Repeat("-", 30)} {
		if got, ok := cleanPhone(bad); ok {
			t.Errorf("cleanPhone(%q) = %q, want a refusal", bad, got)
		}
	}
}

func TestCheckPlace(t *testing.T) {
	long := "https://example.org/" + strings.Repeat("a", 500)
	cases := []struct {
		kind, place, want string
	}{
		{PlaceOnline, "", msgLinkNeeded},
		{PlaceOnsite, "", msgAddressNeeded},
		{PlaceOnline, "https://meet.example.org/abc", ""},
		{PlaceOnline, "http://meet.example.org/abc", ""},
		{PlaceOnline, "ftp://example.org/x", msgLinkBad},
		{PlaceOnline, "javascript:alert(1)", msgLinkBad},
		{PlaceOnline, "meet.example.org/abc", msgLinkBad},
		{PlaceOnline, "https://", msgLinkBad},
		{PlaceOnline, "https://example.org/a b", msgLinkBad},
		{PlaceOnline, "https://exa mple", msgLinkBad},
		{PlaceOnline, "%zz", msgLinkBad},
		{PlaceOnline, long, msgPlaceLong},
		{PlaceOnsite, "Новосибирск, пр. Академика Лаврентьева, 5, каб. 12", ""},
		{PlaceOnsite, strings.Repeat("я", 501), msgPlaceLong},
		{PlaceOnsite, strings.Repeat("я", 500), ""},
	}
	for _, c := range cases {
		if got := checkPlace(c.kind, c.place); got != c.want {
			t.Errorf("checkPlace(%s, %.30q) = %q, want %q", c.kind, c.place, got, c.want)
		}
	}
}

func TestValidateInvitation(t *testing.T) {
	good := InvitationInput{Kind: InvInterview, StartsAt: at(48 * time.Hour), PlaceKind: PlaceOnline, Place: "https://meet.example.org/x"}
	contacts := InvitationInput{Kind: InvContacts, ContactEmail: "Hr@Example.ru", ContactName: "  Ольга   Кузнецова "}
	cases := []struct {
		name   string
		in     InvitationInput
		fields []string // поля с ошибками
	}{
		{"interview ok", good, nil},
		{"onsite ok", InvitationInput{Kind: InvInterview, StartsAt: at(time.Hour), PlaceKind: PlaceOnsite, Place: "Москва, ул. Науки, 1"}, nil},
		{"unknown kind", InvitationInput{Kind: "call"}, []string{"kind"}},
		{"no kind", InvitationInput{}, []string{"kind"}},
		{"interview without anything", InvitationInput{Kind: InvInterview}, []string{"starts_at", "place_kind"}},
		{"interview past", InvitationInput{Kind: InvInterview, StartsAt: at(-time.Hour), PlaceKind: PlaceOnsite, Place: "x"}, []string{"starts_at"}},
		{"interview bad format", InvitationInput{Kind: InvInterview, StartsAt: at(time.Hour), PlaceKind: "phone", Place: "x"}, []string{"place_kind"}},
		{"interview online bad link", InvitationInput{Kind: InvInterview, StartsAt: at(time.Hour), PlaceKind: PlaceOnline, Place: "zoom"}, []string{"place"}},
		{"interview no place", InvitationInput{Kind: InvInterview, StartsAt: at(time.Hour), PlaceKind: PlaceOnsite}, []string{"place"}},
		{"message too long", InvitationInput{Kind: InvRequest, Message: strings.Repeat("я", 2001)}, []string{"message"}},
		{"message at the limit", InvitationInput{Kind: InvRequest, Message: strings.Repeat("я", 2000)}, nil},
		{"request without message", InvitationInput{Kind: InvRequest}, nil},
		{"contacts ok", contacts, nil},
		{"contacts phone only", InvitationInput{Kind: InvContacts, ContactPhone: "+7 913 000-00-00"}, nil},
		{"contacts nothing", InvitationInput{Kind: InvContacts, ContactName: "Ольга"}, []string{"contact_email"}},
		{"contacts bad email", InvitationInput{Kind: InvContacts, ContactEmail: "not-mail"}, []string{"contact_email"}},
		{"contacts bad phone", InvitationInput{Kind: InvContacts, ContactPhone: "позвоните"}, []string{"contact_phone"}},
		{"contacts long name", InvitationInput{Kind: InvContacts, ContactEmail: "a@b.ru", ContactName: strings.Repeat("я", 121)}, []string{"contact_name"}},
		{"contacts everything wrong", InvitationInput{Kind: InvContacts, ContactEmail: "x", ContactPhone: "y", ContactName: strings.Repeat("я", 121)}, []string{"contact_email", "contact_name", "contact_phone"}},
	}
	for _, c := range cases {
		_, errs := validateInvitation(c.in, fixedNow)
		got := make([]string, 0, len(errs))
		for f := range errs {
			got = append(got, f)
		}
		if len(got) != len(c.fields) {
			t.Errorf("%s: fields = %v, want %v", c.name, got, c.fields)
			continue
		}
		for _, f := range c.fields {
			if errs[f] == "" {
				t.Errorf("%s: no error for %q (got %v)", c.name, f, errs)
			}
		}
	}

	// Нормализация и отбрасывание чужих полей.
	v, errs := validateInvitation(contacts, fixedNow)
	if len(errs) != 0 || v.ContactEmail != "hr@example.ru" || v.ContactName != "Ольга Кузнецова" || v.StartsAt != nil || v.PlaceKind != nil {
		t.Errorf("contacts = %+v, %v", v, errs)
	}
	mixed := good
	mixed.ContactEmail, mixed.ContactName, mixed.ContactPhone = "x", "y", "z"
	v, errs = validateInvitation(mixed, fixedNow)
	if len(errs) != 0 || v.ContactEmail != "" || v.Phone != "" || v.StartsAt == nil || *v.PlaceKind != PlaceOnline {
		t.Errorf("interview with foreign fields = %+v, %v", v, errs)
	}
	req := InvitationInput{Kind: InvRequest, StartsAt: "garbage", Place: "p", PlaceKind: "bad", ContactEmail: "bad"}
	if v, errs := validateInvitation(req, fixedNow); len(errs) != 0 || v.StartsAt != nil || v.Place != "" {
		t.Errorf("request with foreign fields = %+v, %v", v, errs)
	}
}

func TestValidateAnswer(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		in     AnswerInput
		status string
		fields []string
	}{
		{"confirm", InvInterview, AnswerInput{Action: AnswerConfirm}, InvConfirmed, nil},
		{"confirm with note", InvInterview, AnswerInput{Action: AnswerConfirm, Note: " Буду "}, InvConfirmed, nil},
		{"propose", InvInterview, AnswerInput{Action: AnswerPropose, ProposedAt: at(72 * time.Hour)}, InvProposed, nil},
		{"propose without time", InvInterview, AnswerInput{Action: AnswerPropose}, InvProposed, []string{"proposed_at"}},
		{"propose past", InvInterview, AnswerInput{Action: AnswerPropose, ProposedAt: at(-time.Hour)}, InvProposed, []string{"proposed_at"}},
		{"propose far", InvInterview, AnswerInput{Action: AnswerPropose, ProposedAt: at(400 * 24 * time.Hour)}, InvProposed, []string{"proposed_at"}},
		{"reply", InvRequest, AnswerInput{Action: AnswerReply, Contact: " +7 913 000-00-00 ", Time: "после 15:00"}, InvAnswered, nil},
		{"reply without contact", InvRequest, AnswerInput{Action: AnswerReply, Time: "утром"}, InvAnswered, []string{"contact"}},
		{"reply long contact", InvRequest, AnswerInput{Action: AnswerReply, Contact: strings.Repeat("я", 301)}, InvAnswered, []string{"contact"}},
		{"reply long time", InvRequest, AnswerInput{Action: AnswerReply, Contact: "x", Time: strings.Repeat("я", 301)}, InvAnswered, []string{"time"}},
		{"long note", InvInterview, AnswerInput{Action: AnswerConfirm, Note: strings.Repeat("я", 1001)}, InvConfirmed, []string{"note"}},
		{"confirm for a request", InvRequest, AnswerInput{Action: AnswerConfirm}, "", []string{"action"}},
		{"reply for an interview", InvInterview, AnswerInput{Action: AnswerReply, Contact: "x"}, "", []string{"action"}},
		{"anything for contacts", InvContacts, AnswerInput{Action: AnswerConfirm}, "", []string{"action"}},
		{"no action", InvInterview, AnswerInput{}, "", []string{"action"}},
	}
	for _, c := range cases {
		v, errs := validateAnswer(c.kind, c.in, fixedNow)
		if len(errs) != len(c.fields) {
			t.Errorf("%s: errors = %v, want fields %v", c.name, errs, c.fields)
			continue
		}
		for _, f := range c.fields {
			if errs[f] == "" {
				t.Errorf("%s: no error for %q (got %v)", c.name, f, errs)
			}
		}
		if len(c.fields) == 0 && v.Status != c.status {
			t.Errorf("%s: status = %q, want %q", c.name, v.Status, c.status)
		}
	}
	v, _ := validateAnswer(InvRequest, AnswerInput{Action: AnswerReply, Contact: "  a   b ", Time: " c  d "}, fixedNow)
	if v.Contact != "a b" || v.Time != "c d" {
		t.Errorf("answer was not normalized: %+v", v)
	}
	v, _ = validateAnswer(InvInterview, AnswerInput{Action: AnswerConfirm, ProposedAt: at(time.Hour), Contact: "x"}, fixedNow)
	if v.ProposedAt != nil || v.Contact != "" {
		t.Errorf("foreign answer fields must be dropped: %+v", v)
	}
}

func TestMomentText(t *testing.T) {
	got := momentText(time.Date(2026, 10, 15, 11, 5, 0, 0, time.UTC))
	if got != "15 октября 2026, 14:05 (МСК)" {
		t.Errorf("momentText = %q", got)
	}
}
