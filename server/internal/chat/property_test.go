package chat

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"scibox/server/internal/testkit"
)

// Проверки на множестве входных данных (срез 40, T-98, T-99): правила сверяются не на нескольких примерах, а на сотнях
// случайных наборов, и при сбое rapid сжимает пример до самого короткого.

// ---- ключ листания ----

func TestCursorProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		micros := rapid.Int64Range(-1<<50, 1<<50).Draw(rt, "micros")
		id := uuid.UUID(rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, "id"))
		at := time.UnixMicro(micros).UTC()
		gotAt, gotID, ok := decodeCursor(encodeCursor(at, id))
		if !ok || !gotAt.Equal(at) || gotID != id {
			rt.Fatalf("decode(encode(%v, %v)) = %v, %v, %v", at, id, gotAt, gotID, ok)
		}
	})
}

// Любая строка из адреса разбирается без паники; принятый ключ при повторной упаковке даёт то же место.
func TestCursorDecodeIsTotalAndStable(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.OneOf(rapid.String(), rapid.StringMatching(`[A-Za-z0-9_-]{0,80}`)).Draw(rt, "cursor")
		at, id, ok := decodeCursor(s)
		if !ok {
			return
		}
		at2, id2, ok2 := decodeCursor(encodeCursor(at, id))
		if !ok2 || !at2.Equal(at) || id2 != id {
			rt.Fatalf("accepted %q but its place does not survive re-encoding", s)
		}
	})
}

// ---- проверка текста ----

func TestCleanTextProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.OneOf(rapid.String(), rapid.StringOfN(rapid.RuneFrom([]rune{'a', 'я', ' ', '\n', '\r', '\t', '\x00', '‮', '⁦', 'ё', '🙂', '​'}), 0, 40, -1)).Draw(rt, "raw")
		text, problem := cleanText(raw)
		if problem != "" {
			if text != "" {
				rt.Fatalf("a refused text comes back with a value %q", text)
			}
			return
		}
		switch {
		case text == "" || text != strings.TrimSpace(text):
			rt.Fatalf("accepted text is empty or not trimmed: %q", text)
		case utf8.RuneCountInString(text) > MaxText:
			rt.Fatalf("accepted text is too long: %d", utf8.RuneCountInString(text))
		case !utf8.ValidString(text) || strings.ContainsRune(text, '\r'):
			rt.Fatalf("accepted text has a broken or CR character: %q", text)
		}
		for _, r := range text {
			if (unicode.IsControl(r) && r != '\n' && r != '\t') || isBidiControl(r) {
				rt.Fatalf("accepted text holds %U: %q", r, text)
			}
		}
		// Идемпотентность: принятый текст принимается без изменений.
		again, p2 := cleanText(text)
		if p2 != "" || again != text {
			rt.Fatalf("cleanText is not idempotent: %q → %q (%s)", text, again, p2)
		}
	})
}

// Чужой текст в письме: одна строка, без управляющих знаков, не длиннее предела, предел по знакам, а не по байтам.
func TestMailSafeProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := rapid.String().Draw(rt, "in")
		limit := rapid.IntRange(2, 60).Draw(rt, "limit")
		out := mailSafe(in, limit)
		if utf8.RuneCountInString(out) > limit {
			rt.Fatalf("%q is longer than %d", out, limit)
		}
		for _, r := range out {
			if unicode.IsControl(r) || isBidiControl(r) || r == ' ' && false {
				rt.Fatalf("mailSafe(%q) keeps %U", in, r)
			}
		}
		if strings.ContainsAny(out, "\n\r") || out != strings.TrimSpace(out) || strings.Contains(out, "  ") {
			rt.Fatalf("mailSafe(%q) = %q is not a single tidy line", in, out)
		}
		if again := mailSafe(out, limit); again != out {
			rt.Fatalf("mailSafe is not idempotent: %q → %q", out, again)
		}
	})
}

// ---- листание: без пропусков и повторов ----

// Список переписок, пролистанный страницами любого размера, равен списку одним запросом, без повторов и пропусков, в том
// числе когда у многих переписок одно и то же время (ключ — время и номер).
func TestListPagingProperty(t *testing.T) {
	w := newWorld(t)
	var ids []uuid.UUID
	for i := range 14 {
		ids = append(ids, w.chatOf(w.Applicant(fmt.Sprintf("Учёный %d", i)), w.vacancy.ID, "sent"))
	}
	owner := w.tm.Owner
	full, err := w.svc.List(bg, owner.User, ListQuery{Side: "org", Limit: maxList})
	if err != nil || full.NextCursor != nil || len(full.Items) != len(ids) {
		t.Fatalf("full list = %d items, cursor %v, err %v", len(full.Items), full.NextCursor, err)
	}
	rapid.Check(t, func(rt *rapid.T) {
		// Случайно склеиваем времена в несколько корзин: ничьи в порядке по времени решает номер.
		buckets := rapid.IntRange(1, 5).Draw(rt, "buckets")
		base := time.Now().UTC().Truncate(time.Microsecond)
		for i, id := range ids {
			b := rapid.IntRange(0, buckets-1).Draw(rt, fmt.Sprintf("bucket %d", i))
			if _, err := testkit.Pool.Exec(bg, `UPDATE chats SET updated_at = $2 WHERE id = $1`, id, base.Add(time.Duration(b)*time.Second)); err != nil {
				rt.Fatal(err)
			}
		}
		want, err := w.svc.List(bg, owner.User, ListQuery{Side: "org", Limit: maxList})
		if err != nil {
			rt.Fatal(err)
		}
		limit := rapid.IntRange(1, 6).Draw(rt, "limit")
		var got []uuid.UUID
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > len(ids)+1 {
				rt.Fatalf("paging does not end")
			}
			page, err := w.svc.List(bg, owner.User, ListQuery{Side: "org", Limit: limit, Cursor: cursor})
			if err != nil {
				rt.Fatal(err)
			}
			if len(page.Items) > limit {
				rt.Fatalf("page of %d with limit %d", len(page.Items), limit)
			}
			for _, it := range page.Items {
				got = append(got, it.ID)
			}
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		var wantIDs []uuid.UUID
		for _, it := range want.Items {
			wantIDs = append(wantIDs, it.ID)
		}
		if !slices.Equal(got, wantIDs) {
			rt.Fatalf("paged = %v\nwhole = %v", got, wantIDs)
		}
	})
}

// Лента, пролистанная назад по before и вперёд по after страницами любого размера, даёт номера 1..n без пропусков и повторов.
func TestMessagesPagingProperty(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	const n = 23
	for i := range n {
		who := sci
		if i%7 < 3 {
			who = w.tm.HR
		}
		w.send(who, chat, fmt.Sprintf("Сообщение %d", i))
	}
	total := n
	rapid.Check(t, func(rt *rapid.T) {
		limit := rapid.IntRange(1, 9).Draw(rt, "limit")
		// Назад от конца.
		var back []int
		var before *int
		for pages := 0; ; pages++ {
			if pages > total+1 {
				rt.Fatalf("paging back does not end")
			}
			page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{Limit: limit, Before: before})
			if err != nil {
				rt.Fatal(err)
			}
			for i := len(page.Items) - 1; i >= 0; i-- {
				back = append(back, page.Items[i].Seq)
			}
			if !page.HasMore {
				break
			}
			first := page.Items[0].Seq
			before = &first
		}
		for i, seq := range back {
			if seq != total-i {
				rt.Fatalf("backwards: %v", back)
			}
		}
		if len(back) != total {
			rt.Fatalf("backwards got %d of %d: %v", len(back), total, back)
		}
		// Вперёд с нуля.
		var fwd []int
		after := 0
		for pages := 0; ; pages++ {
			if pages > total+1 {
				rt.Fatalf("paging forward does not end")
			}
			a := after
			page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{Limit: limit, After: &a})
			if err != nil {
				rt.Fatal(err)
			}
			for _, m := range page.Items {
				fwd = append(fwd, m.Seq)
			}
			if !page.HasMore {
				break
			}
			after = page.Items[len(page.Items)-1].Seq
		}
		for i, seq := range fwd {
			if seq != i+1 {
				rt.Fatalf("forwards: %v", fwd)
			}
		}
		if len(fwd) != total {
			rt.Fatalf("forwards got %d of %d", len(fwd), total)
		}
	})
}

// ---- права и состояния: модель против настоящей службы ----

// Случайные последовательности действий случайных людей в переписке, в которой то открывается, то закрывается отклик.
// Модель повторяет правила CHAT.md отдельно от кода; расхождение в исходе любого действия — ошибка. Чужой участник не
// получает ничего и ничего не меняет, закрытая переписка не растёт, счёт подряд без ответа считается по стороне.
func TestChatModelProperty(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(DefaultLimits())
	vacancies := []uuid.UUID{}
	for range 3 {
		vacancies = append(vacancies, w.tm.Published(&w.tm.UnitA.ID).ID)
	}
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		sci := w.Applicant(fmt.Sprintf("Учёный %d", n))
		app := w.application(sci, vacancies[n%len(vacancies)], "sent")
		chat := w.open(sci, vacancies[n%len(vacancies)], &app, nil)

		type msg struct {
			author  string
			side    Side
			text    string
			at      time.Time
			deleted bool
		}
		var model []msg
		closed := false
		actors := map[string]testkit.Person{"sci": sci, "owner": w.tm.Owner, "hr": w.tm.HR, "headA": w.tm.HeadA, "headB": w.tm.HeadB, "out": w.tm.Out}
		names := []string{"sci", "owner", "hr", "headA", "headB", "out"}
		sideOf := func(a string) Side {
			if a == "sci" {
				return SideScientist
			}
			return SideOrg
		}
		member := func(a string) bool { return a != "headB" && a != "out" }
		streak := func(side Side) int {
			c := 0
			for i := len(model) - 1; i >= 0 && model[i].side == side; i-- {
				c++
			}
			return c
		}
		// Переписка заведена без строк о событиях: тексты идут с номера 1.
		seqOf := func(i int) int { return i + 1 }

		rt.Repeat(map[string]func(*rapid.T){
			"send": func(rt *rapid.T) {
				a := rapid.SampledFrom(names).Draw(rt, "who")
				text := rapid.StringMatching(`[а-я]{1,12}`).Draw(rt, "text")
				_, _, err := w.svc.Send(bg, actors[a].User, chat, SendInput{Text: text, ClientID: uuid.NewString()})
				switch {
				case !member(a):
					if !errors.Is(err, ErrNotFound) {
						rt.Fatalf("%s sends: %v, want ErrNotFound", a, err)
					}
				case closed:
					if !errors.Is(err, ErrClosed) {
						rt.Fatalf("%s sends into a closed chat: %v", a, err)
					}
				case streak(sideOf(a)) >= DefaultUnanswered:
					if !errors.Is(err, ErrAwaitingReply) {
						rt.Fatalf("%s sends a sixth text in a row: %v", a, err)
					}
				default:
					if err != nil {
						rt.Fatalf("%s sends: %v", a, err)
					}
					model = append(model, msg{author: a, side: sideOf(a), text: text, at: w.clock.Now()})
				}
			},
			"edit": func(rt *rapid.T) {
				a := rapid.SampledFrom(names).Draw(rt, "who")
				i := rapid.IntRange(0, len(model)).Draw(rt, "index")
				_, err := w.svc.Edit(bg, actors[a].User, chat, seqOf(i), EditInput{Text: "правка"})
				switch {
				case !member(a):
					if !errors.Is(err, ErrNotFound) {
						rt.Fatalf("%s edits: %v, want ErrNotFound", a, err)
					}
				case i >= len(model):
					if !errors.Is(err, ErrMessageNotFound) {
						rt.Fatalf("%s edits a missing message: %v", a, err)
					}
				case model[i].author != a:
					if !errors.Is(err, ErrNotAuthor) {
						rt.Fatalf("%s edits a message of %s: %v", a, model[i].author, err)
					}
				case model[i].deleted:
					if !errors.Is(err, ErrMessageNotFound) {
						rt.Fatalf("editing a deleted message: %v", err)
					}
				case closed:
					if !errors.Is(err, ErrClosed) {
						rt.Fatalf("editing in a closed chat: %v", err)
					}
				case w.clock.Now().After(model[i].at.Add(EditWindow)):
					if !errors.Is(err, ErrEditWindowClosed) {
						rt.Fatalf("editing after the window: %v", err)
					}
				default:
					if err != nil {
						rt.Fatalf("%s edits their own message: %v", a, err)
					}
					model[i].text = "правка"
				}
			},
			"delete": func(rt *rapid.T) {
				a := rapid.SampledFrom(names).Draw(rt, "who")
				i := rapid.IntRange(0, len(model)).Draw(rt, "index")
				err := w.svc.Delete(bg, actors[a].User, chat, seqOf(i))
				switch {
				case !member(a):
					if !errors.Is(err, ErrNotFound) {
						rt.Fatalf("%s deletes: %v, want ErrNotFound", a, err)
					}
				case i >= len(model):
					if !errors.Is(err, ErrMessageNotFound) {
						rt.Fatalf("%s deletes a missing message: %v", a, err)
					}
				case model[i].author != a:
					if !errors.Is(err, ErrNotAuthor) {
						rt.Fatalf("%s deletes a message of %s: %v", a, model[i].author, err)
					}
				case model[i].deleted:
					if err != nil {
						rt.Fatalf("a repeated deletion must change nothing and succeed: %v", err)
					}
				case closed:
					if !errors.Is(err, ErrClosed) {
						rt.Fatalf("deleting in a closed chat: %v", err)
					}
				case w.clock.Now().After(model[i].at.Add(EditWindow)):
					if !errors.Is(err, ErrEditWindowClosed) {
						rt.Fatalf("deleting after the window: %v", err)
					}
				default:
					if err != nil {
						rt.Fatalf("%s deletes their own message: %v", a, err)
					}
					model[i].deleted = true
					model[i].text = ""
				}
			},
			"read": func(rt *rapid.T) {
				a := rapid.SampledFrom(names).Draw(rt, "who")
				upTo := rapid.IntRange(0, len(model)+10).Draw(rt, "up to")
				err := w.svc.MarkRead(bg, actors[a].User, chat, upTo)
				if member(a) != (err == nil) || (!member(a) && !errors.Is(err, ErrNotFound)) {
					rt.Fatalf("%s marks read: %v", a, err)
				}
			},
			"decide": func(rt *rapid.T) {
				status := rapid.SampledFrom([]string{"sent", "viewed", "accepted", "rejected", "withdrawn"}).Draw(rt, "status")
				w.setApplication(app, status)
				closed = status == "rejected" || status == "withdrawn"
			},
			"wait": func(rt *rapid.T) {
				w.clock.Advance(time.Duration(rapid.IntRange(1, 20).Draw(rt, "hours")) * time.Hour)
			},
			"": func(rt *rapid.T) {
				// Инварианты после каждого шага: ленту сторонние не видят, ленту участников совпадает с моделью.
				for _, a := range names {
					page, err := w.svc.Messages(bg, actors[a].User, chat, MessagesQuery{Limit: 100})
					if !member(a) {
						if !errors.Is(err, ErrNotFound) {
							rt.Fatalf("%s reads the feed: %v", a, err)
						}
						continue
					}
					if err != nil {
						rt.Fatalf("%s reads the feed: %v", a, err)
					}
					var got []string
					for _, m := range page.Items {
						if m.Kind == KindText {
							got = append(got, m.Text)
						}
					}
					var want []string
					for _, m := range model {
						want = append(want, m.text)
					}
					if !slices.Equal(got, want) {
						rt.Fatalf("%s sees %q, model %q", a, got, want)
					}
					for i, m := range page.Items {
						if m.Seq != i+1 {
							rt.Fatalf("numbers are not 1..n: %d at %d", m.Seq, i)
						}
					}
				}
				if got := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1 AND kind = 'text'`, chat); got != len(model) {
					rt.Fatalf("%d texts in the database, model %d", got, len(model))
				}
			},
		})
	})
}
