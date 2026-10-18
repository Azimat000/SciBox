package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStateOf(t *testing.T) {
	cases := []struct {
		name        string
		app, offer  *string
		source, val string
		closed      bool
		reason      string
	}{
		{"application sent", str("sent"), nil, "application", "sent", false, ""},
		{"application viewed", str("viewed"), nil, "application", "viewed", false, ""},
		{"application accepted stays open", str("accepted"), nil, "application", "accepted", false, ""},
		{"application rejected", str("rejected"), nil, "application", "rejected", true, ClosedRejected},
		{"application withdrawn", str("withdrawn"), nil, "application", "withdrawn", true, ClosedWithdrawn},
		{"offer pending", nil, str("pending"), "offer", "pending", false, ""},
		{"offer interested", nil, str("interested"), "offer", "interested", false, ""},
		{"offer declined", nil, str("declined"), "offer", "declined", true, ClosedOfferDeclined},
		{"offer cancelled", nil, str("cancelled"), "offer", "cancelled", true, ClosedOfferCancelled},
		// Отклик главнее приглашения: после «Не сейчас» человек может откликнуться, и переписка снова живая.
		{"application over a declined offer", str("sent"), str("declined"), "application", "sent", false, ""},
		{"rejected application over an interested offer", str("rejected"), str("interested"), "application", "rejected", true, ClosedRejected},
		{"nothing at all", nil, nil, "application", "withdrawn", true, ClosedWithdrawn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := StateOf(c.app, c.offer, false)
			if got.Source != c.source || got.Value != c.val || got.Closed != c.closed || got.Reason != c.reason {
				t.Errorf("StateOf = %+v", got)
			}
		})
	}
}

// Блокировка закрывает открытую переписку; закрытая решением остаётся закрытой по прежней причине (D-237).
func TestStateOfBlocked(t *testing.T) {
	cases := []struct {
		name       string
		app, offer *string
		closed     bool
		reason     string
	}{
		{"open application", str("viewed"), nil, true, ClosedBlocked},
		{"accepted application", str("accepted"), nil, true, ClosedBlocked},
		{"pending offer", nil, str("pending"), true, ClosedBlocked},
		{"rejected stays rejected", str("rejected"), nil, true, ClosedRejected},
		{"declined offer stays declined", nil, str("declined"), true, ClosedOfferDeclined},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := StateOf(c.app, c.offer, true)
			if got.Closed != c.closed || got.Reason != c.reason {
				t.Errorf("StateOf = %+v", got)
			}
		})
	}
}

// Каждое событие кому-то адресовано, и адресат не тот, кто его совершил: человеку не показывается его же действие
// как непрочитанное.
func TestEveryEventIsAddressed(t *testing.T) {
	if got := len(EventTypes()); got != 13 {
		t.Fatalf("event types = %d, want 13", got)
	}
	byScientist := map[string]bool{EventApplicationSent: true, EventInvitationAnswered: true, EventApplicationWithdrawn: true, EventOfferAnswered: true}
	for _, e := range EventTypes() {
		to := audience[e]
		if !to.Valid() {
			t.Errorf("%s: addressee %q", e, to)
		}
		if byScientist[e] && to != SideOrg {
			t.Errorf("%s is made by the scientist, so it is for the organization", e)
		}
	}
}

func TestCleanText(t *testing.T) {
	long := ""
	for range MaxText {
		long += "я"
	}
	cases := []struct {
		name, in, want, problem string
	}{
		{"plain", "Добрый день!", "Добрый день!", ""},
		{"trimmed", "  привет\n", "привет", ""},
		{"line breaks kept", "строка 1\nстрока 2", "строка 1\nстрока 2", ""},
		{"CRLF becomes LF", "а\r\nб", "а\nб", ""},
		{"tab kept", "а\tб", "а\tб", ""},
		{"empty", "", "", msgEmpty},
		{"spaces only", " \n\t ", "", msgEmpty},
		{"limit", long, long, ""},
		{"over the limit", long + "я", "", msgTooLong},
		{"emoji counts as one", "👍", "👍", ""},
		{"NUL", "а\x00б", "", msgBadChars},
		{"invalid UTF-8", "а\xffб", "", msgBadChars},
		{"escape", "а\x1bб", "", msgBadChars},
		{"right-to-left override", "ссылка \u202eexe.pdf", "", msgBadChars},
		{"isolate", "а\u2066б", "", msgBadChars},
		{"zero width joiner of emoji stays", "👨‍🔬", "👨‍🔬", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, problem := cleanText(c.in)
			if got != c.want || problem != c.problem {
				t.Errorf("cleanText(%q) = %q, %q", c.in, got, problem)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 40, 0, 123456000, time.UTC)
	id := uuid.New()
	gotAt, gotID, ok := decodeCursor(encodeCursor(at, id))
	if !ok || !gotAt.Equal(at) || gotID != id {
		t.Fatalf("round trip: %v %v %v", gotAt, gotID, ok)
	}
	for _, bad := range []string{"", "###", "bm90LWEtY3Vyc29y", "MTIzOg", "MTIzOmJhZA", "eHl6OjAwMDAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwMA"} {
		if _, _, ok := decodeCursor(bad); ok {
			t.Errorf("decodeCursor(%q) accepted", bad)
		}
	}
}
