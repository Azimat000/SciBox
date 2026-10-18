package chat

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
)

func svcOn(w *world, db testkit.DB) *Service {
	s := newService(db)
	s.now = w.clock.Now
	return s
}

type faultPrep func(t *testing.T, w *world) func(db testkit.DB, s *Service) error

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего, а не обернуться
// «нет такой переписки» или тишиной.
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	setup := func(w *world) (testkit.Person, uuid.UUID) {
		sci := w.Applicant("Анна Ильина")
		chat := w.chatOf(sci, w.vacancy.ID, "sent")
		w.system(chat, EventApplicationSent, nil)
		return sci, chat
	}
	cases := map[string]faultPrep{
		"get": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error { _, err := s.Get(bg, sci.User, chat); return err }
		},
		"get as the organization": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			_, chat := setup(w)
			return func(_ testkit.DB, s *Service) error { _, err := s.Get(bg, w.tm.HeadA.User, chat); return err }
		},
		"latest messages": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			w.send(w.tm.HR, chat, "Здравствуйте")
			return func(_ testkit.DB, s *Service) error {
				_, err := s.Messages(bg, sci.User, chat, MessagesQuery{})
				return err
			}
		},
		"newer messages": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			w.send(w.tm.HR, chat, "Здравствуйте")
			return func(_ testkit.DB, s *Service) error {
				after := 0
				_, err := s.Messages(bg, sci.User, chat, MessagesQuery{After: &after})
				return err
			}
		},
		"send": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error {
				_, _, err := s.Send(bg, sci.User, chat, SendInput{Text: "Привет", ClientID: uuid.NewString()})
				return err
			}
		},
		"send with a repeat": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			client := uuid.NewString()
			w.svcSend(sci, chat, client)
			return func(_ testkit.DB, s *Service) error {
				_, _, err := s.Send(bg, sci.User, chat, SendInput{Text: "Привет", ClientID: client})
				return err
			}
		},
		"send as a colleague with a deleted account": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			_, chat := setup(w)
			return func(_ testkit.DB, s *Service) error {
				_, _, err := s.Send(bg, w.tm.Owner.User, chat, SendInput{Text: "От организации", ClientID: uuid.NewString()})
				return err
			}
		},
		"send in the chat of an offer": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Борис Сомов")
			offer := w.offer(sci, w.vacancy.ID, "pending")
			chat := w.open(sci, w.vacancy.ID, nil, &offer)
			return func(_ testkit.DB, s *Service) error {
				_, _, err := s.Send(bg, sci.User, chat, SendInput{Text: "Привет", ClientID: uuid.NewString()})
				return err
			}
		},
		"send in a chat relinked while waiting": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Вера Светлова")
			offer := w.offer(sci, w.vacancy.ID, "pending")
			chat := w.open(sci, w.vacancy.ID, nil, &offer)
			app := w.application(sci, w.vacancy.ID, "sent")
			return func(db testkit.DB, s *Service) error {
				if f, ok := db.(*testkit.FaultDB); ok {
					relinked := false
					f.Hook = func(sql string) {
						if !relinked && hasAll(sql, "FROM chats WHERE id", "FOR UPDATE") {
							relinked = true
							if _, err := testkit.Pool.Exec(bg, `UPDATE chats SET application_id = $2 WHERE id = $1`, chat, app); err != nil {
								t.Error(err)
							}
						}
					}
				}
				_, _, err := s.Send(bg, sci.User, chat, SendInput{Text: "Привет", ClientID: uuid.NewString()})
				return err
			}
		},
		"edit": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			m := w.send(sci, chat, "Первый вариант")
			return func(_ testkit.DB, s *Service) error {
				_, err := s.Edit(bg, sci.User, chat, m.Seq, EditInput{Text: "Второй вариант"})
				return err
			}
		},
		"delete": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			m := w.send(sci, chat, "Лишнее")
			return func(_ testkit.DB, s *Service) error { return s.Delete(bg, sci.User, chat, m.Seq) }
		},
		"read": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error { return s.MarkRead(bg, sci.User, chat, 1) }
		},
		"archive": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error { return s.Archive(bg, sci.User, chat, true) }
		},
		"typing": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error { return s.Typing(bg, sci.User, chat) }
		},
		"list of the scientist": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, _ := setup(w)
			return func(_ testkit.DB, s *Service) error {
				_, err := s.List(bg, sci.User, ListQuery{Side: "scientist", Search: "лаборатория"})
				return err
			}
		},
		"list of the organization": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			setup(w)
			return func(_ testkit.DB, s *Service) error {
				_, err := s.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Box: BoxUnread})
				return err
			}
		},
		"unread count": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			setup(w)
			return func(_ testkit.DB, s *Service) error { _, err := s.UnreadCount(bg, w.tm.Owner.User); return err }
		},
		"vacancies": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			setup(w)
			return func(_ testkit.DB, s *Service) error { _, err := s.Vacancies(bg, w.tm.Owner.User); return err }
		},
		"block": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error { _, err := s.Block(bg, sci.User, chat); return err }
		},
		"unblock": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			if _, err := w.svc.Block(bg, sci.User, chat); err != nil {
				t.Fatal(err)
			}
			return func(_ testkit.DB, s *Service) error { return s.Unblock(bg, sci.User, w.tm.OrgID) }
		},
		"blocks": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			if _, err := w.svc.Block(bg, sci.User, chat); err != nil {
				t.Fatal(err)
			}
			return func(_ testkit.DB, s *Service) error { _, err := s.Blocks(bg, sci.User); return err }
		},
		"complaint": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci, chat := setup(w)
			return func(_ testkit.DB, s *Service) error {
				_, err := s.Complain(bg, sci.User, chat, ComplaintInput{Reason: "spam", Note: "Пишут не по делу"})
				return err
			}
		},
		"templates": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			return func(_ testkit.DB, s *Service) error {
				_, err := s.Templates(bg, w.tm.HeadA.User, w.tm.OrgID)
				return err
			}
		},
		"template create": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			return func(_ testkit.DB, s *Service) error {
				_, err := s.CreateTemplate(bg, w.tm.Owner.User, w.tm.OrgID, TemplateInput{Title: "Шаблон", Text: "Текст"})
				return err
			}
		},
		"template update": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			tpl, err := w.svc.CreateTemplate(bg, w.tm.Owner.User, w.tm.OrgID, TemplateInput{Title: "Шаблон", Text: "Текст"})
			if err != nil {
				t.Fatal(err)
			}
			return func(_ testkit.DB, s *Service) error {
				_, err := s.UpdateTemplate(bg, w.tm.HR.User, w.tm.OrgID, tpl.ID, TemplateInput{Title: "Новое", Text: "Новый текст"})
				return err
			}
		},
		"template delete": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			tpl, err := w.svc.CreateTemplate(bg, w.tm.Owner.User, w.tm.OrgID, TemplateInput{Title: "Шаблон", Text: "Текст"})
			if err != nil {
				t.Fatal(err)
			}
			return func(_ testkit.DB, s *Service) error { return s.DeleteTemplate(bg, w.tm.Owner.User, w.tm.OrgID, tpl.ID) }
		},
		"links": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Анна Ильина")
			app := w.application(sci, w.vacancy.ID, "sent")
			offer := w.offer(sci, w.other.ID, "pending")
			w.open(sci, w.vacancy.ID, &app, nil)
			return func(db testkit.DB, _ *Service) error {
				_, err := LinksOf(bg, dbgen.New(db), SideOrg, w.tm.Owner.ID, []uuid.UUID{app}, []uuid.UUID{offer})
				return err
			}
		},
		"record into a new chat": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Борис Сомов")
			app := w.application(sci, w.vacancy.ID, "sent")
			return func(db testkit.DB, _ *Service) error {
				_, err := Record(bg, dbgen.New(db), Entry{VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &app,
					Type: EventApplicationSent, Data: ApplicationSentData{ApplicationID: app}, At: w.clock.Now()})
				return err
			}
		},
		"record into an offer chat": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Борис Сомов")
			offer := w.offer(sci, w.vacancy.ID, "pending")
			w.open(sci, w.vacancy.ID, nil, &offer)
			return func(db testkit.DB, _ *Service) error {
				_, err := Record(bg, dbgen.New(db), Entry{VacancyID: w.vacancy.ID, UserID: sci.ID, OfferID: &offer,
					Type: EventOfferCancelled, Data: OfferData{OfferID: offer}, At: w.clock.Now()})
				return err
			}
		},
		"backfill of a pair": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Борис Сомов")
			w.application(sci, w.vacancy.ID, "viewed")
			offer := w.offer(sci, w.vacancy.ID, "interested")
			_ = offer
			return func(db testkit.DB, _ *Service) error {
				_, err := backfillPair(bg, db, w.vacancy.ID, sci.ID)
				return err
			}
		},
		"open": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			sci := w.Applicant("Борис Сомов")
			app := w.application(sci, w.vacancy.ID, "sent")
			return func(db testkit.DB, _ *Service) error {
				_, err := Open(bg, dbgen.New(db), OpenInput{VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &app}, w.clock.Now())
				return err
			}
		},
		"append a system line": func(t *testing.T, w *world) func(testkit.DB, *Service) error {
			_, chat := setup(w)
			return func(db testkit.DB, _ *Service) error {
				_, err := AppendSystem(bg, dbgen.New(db), chat, EventApplicationViewed, nil, w.clock.Now())
				return err
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			testkit.RunFaults(t, func(t *testing.T) func(testkit.DB) error {
				w := newWorld(t)
				call := prepare(t, w)
				return func(db testkit.DB) error { return call(db, svcOn(w, db)) }
			})
		})
	}
}

func (w *world) svcSend(who testkit.Person, chat uuid.UUID, client string) {
	w.T.Helper()
	if _, _, err := w.svc.Send(bg, who.User, chat, SendInput{Text: "Привет", ClientID: client}); err != nil {
		w.T.Fatal(err)
	}
}

// Сбой на первом же обращении к базе при переносе не молчит.
func TestBackfillReportsDatabaseFailures(t *testing.T) {
	newWorld(t)
	db, _ := testkit.Faulty(testkit.Pool, 1)
	if _, err := Backfill(bg, db); !errors.Is(err, testkit.ErrFault) {
		t.Fatalf("Backfill = %v", err)
	}
}
