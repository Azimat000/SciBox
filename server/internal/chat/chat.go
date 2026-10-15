package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
)

// Пределы ленты.
const (
	defaultMessages = 50
	maxMessages     = 100
)

// counterpartOf — собеседник глазами смотрящего. Организации имя учёного показывается только по правилам privacy
// (анонимный режим, «скрыть от организации», D-199, D-200); учёному собеседник всегда организация.
func counterpartOf(side Side, orgName, scientistName string, profileID *uuid.UUID, visibility *string, hiddenFrom, revealed bool) Counterpart {
	if side == SideScientist {
		return Counterpart{Kind: SideOrg, Name: orgName}
	}
	c := Counterpart{Kind: SideScientist}
	if !nameSeen(visibility, hiddenFrom, revealed) {
		c.Anonymous = true
		return c
	}
	c.Name = scientistName
	if profileSeen(visibility, hiddenFrom, revealed) {
		c.ProfileID = profileID
	}
	return c
}

func needsAnswer(side Side, st State, hasPendingInvitation bool, offerStatus *string) bool {
	if side != SideScientist || st.Closed {
		return false
	}
	if st.Source == sourceApplication {
		return hasPendingInvitation
	}
	return offerStatus != nil && *offerStatus == "pending"
}

func reasonPtr(st State) *string {
	if st.Reason == "" {
		return nil
	}
	return &st.Reason
}

// Get возвращает шапку переписки. Открытие шапки ничего не отмечает прочитанным.
func (s *Service) Get(ctx context.Context, user auth.User, id uuid.UUID) (Chat, error) {
	row, w, err := s.load(ctx, s.q, user, id)
	if err != nil {
		return Chat{}, err
	}
	st := StateOf(row.ApplicationStatus, row.OfferStatus, row.Blocked)
	out := Chat{
		ID: row.ID, Side: w.side,
		Vacancy: VacancyHeader{
			VacancyRef: VacancyRef{ID: row.VacancyID, Title: row.VacancyTitle, OrgName: row.OrgName, OrgSlug: row.OrgSlug, UnitName: deref(row.UnitName)},
			Status:     row.VacancyStatus, Deadline: dateStr(row.Deadline),
		},
		Counterpart:   counterpartOf(w.side, row.OrgName, row.ScientistName, row.ProfileID, row.Visibility, row.HiddenFrom, row.Revealed),
		ApplicationID: row.ApplicationID, OfferID: row.OfferID,
		Status:       Status{Source: st.Source, Value: st.Value},
		NeedsAnswer:  needsAnswer(w.side, st, row.HasPendingInvitation, row.OfferStatus),
		CanWrite:     !st.Closed,
		ClosedReason: reasonPtr(st),
		Archived:     row.Archived,
		Viewer:       Viewer{Side: w.side},
		Read:         Read{Mine: int(row.ReadSeq), Theirs: int(row.OrgRead)},
	}
	switch w.side {
	case SideScientist:
		out.Viewer.CanBlock, out.Viewer.CanComplain = true, true
	case SideOrg:
		orgID := row.OrgID
		out.Viewer.CanUseTemplates = true
		out.Viewer.CanEditTemplates = w.actor.Can(access.EditChatTemplates, access.NoUnit)
		out.Viewer.OrgID = &orgID
		out.Read.Theirs = int(row.ScientistRead)
	}
	return out, nil
}

// Messages возвращает ленту переписки от старых к новым: последние сообщения, более старые, чем Before, или более
// новые, чем After.
func (s *Service) Messages(ctx context.Context, user auth.User, id uuid.UUID, mq MessagesQuery) (Page, error) {
	limit := mq.Limit
	if limit == 0 {
		limit = defaultMessages
	}
	switch {
	case limit < 1 || limit > maxMessages:
		return Page{}, &BadParamError{Param: "limit"}
	case mq.Before != nil && mq.After != nil:
		return Page{}, &BadParamError{Param: "before"}
	case mq.Before != nil && *mq.Before < 1:
		return Page{}, &BadParamError{Param: "before"}
	case mq.After != nil && *mq.After < 0:
		return Page{}, &BadParamError{Param: "after"}
	}
	row, w, err := s.load(ctx, s.q, user, id)
	if err != nil {
		return Page{}, err
	}
	var rows []dbgen.ChatMessage
	if mq.After != nil {
		rows, err = s.q.ListChatMessagesAfter(ctx, dbgen.ListChatMessagesAfterParams{ChatID: id, After: num.Int32(*mq.After), RowLimit: int32(limit + 1)})
	} else {
		var before *int32
		if mq.Before != nil {
			before = new(num.Int32(*mq.Before))
		}
		rows, err = s.q.ListChatMessagesBefore(ctx, dbgen.ListChatMessagesBeforeParams{ChatID: id, Before: before, RowLimit: int32(limit + 1)})
	}
	if err != nil {
		return Page{}, fmt.Errorf("chat: list messages: %w", err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	if mq.After == nil {
		slices.Reverse(rows) // запрос шёл от новых к старым, лента показывается от старых к новым
	}
	staff, err := s.authors(ctx, s.q, row.OrgID, rows)
	if err != nil {
		return Page{}, err
	}
	seen := w.side == SideScientist || nameSeen(row.Visibility, row.HiddenFrom, row.Revealed)
	out := Page{Items: make([]Message, 0, len(rows)), HasMore: more}
	for _, m := range rows {
		out.Items = append(out.Items, s.toMessage(id, m, user.ID, row.ScientistName, seen, staff))
	}
	return out, nil
}

// authors собирает имена авторов организации одним запросом.
func (s *Service) authors(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID, rows []dbgen.ChatMessage) (map[uuid.UUID]authorInfo, error) {
	var ids []uuid.UUID
	for _, m := range rows {
		if m.AuthorUserID != nil && m.AuthorSide != nil && Side(*m.AuthorSide) == SideOrg && !slices.Contains(ids, *m.AuthorUserID) {
			ids = append(ids, *m.AuthorUserID)
		}
	}
	out := make(map[uuid.UUID]authorInfo, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	got, err := q.ListChatAuthors(ctx, dbgen.ListChatAuthorsParams{OrgID: orgID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("chat: list authors: %w", err)
	}
	for _, a := range got {
		out[a.ID] = authorInfo{name: a.DisplayName, member: a.Member}
	}
	return out, nil
}

// one собирает ответ про одно сообщение внутри транзакции.
func (s *Service) one(ctx context.Context, q *dbgen.Queries, row dbgen.GetChatRow, w who, user auth.User, m dbgen.ChatMessage) (Message, error) {
	staff, err := s.authors(ctx, q, row.OrgID, []dbgen.ChatMessage{m})
	if err != nil {
		return Message{}, err
	}
	seen := w.side == SideScientist || nameSeen(row.Visibility, row.HiddenFrom, row.Revealed)
	return s.toMessage(row.ID, m, user.ID, row.ScientistName, seen, staff), nil
}

// Send отправляет текст. Повтор с тем же client_id возвращает уже созданное сообщение (created == false).
func (s *Service) Send(ctx context.Context, user auth.User, id uuid.UUID, in SendInput) (Message, bool, error) {
	text, problem := cleanText(in.Text)
	fields := map[string]string{}
	if problem != "" {
		fields["text"] = problem
	}
	clientID, err := uuid.Parse(in.ClientID)
	if err != nil {
		fields["client_id"] = "Нужен номер отправки"
	}
	if len(fields) > 0 {
		// Человек, у которого переписки нет, не должен узнать о ней по тому, как проверен текст: сначала права.
		if _, _, err := s.load(ctx, s.q, user, id); err != nil {
			return Message{}, false, err
		}
		return Message{}, false, &auth.ValidationError{Fields: fields}
	}
	var out Message
	created := true
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		row, w, err := s.load(ctx, q, user, id)
		if err != nil {
			return err
		}
		st, err := s.lockState(ctx, q, id, row.ApplicationID, row.OfferID)
		if err != nil {
			return err
		}
		// Повтор отправки: сообщение уже есть, отдаём его, пока переписка ещё не закрыта и после закрытия тоже.
		prev, err := q.FindChatMessageByClient(ctx, dbgen.FindChatMessageByClientParams{ChatID: id, AuthorUserID: &user.ID, ClientID: &clientID})
		if err == nil {
			created = false
			out, err = s.one(ctx, q, row, w, user, prev)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("chat: find by client id: %w", err)
		}
		if st.Closed {
			return ErrClosed
		}
		now := s.now()
		to := w.side.Other()
		seq, err := q.BumpChat(ctx, dbgen.BumpChatParams{ToSide: string(to), Now: now, ID: id})
		if err != nil {
			return fmt.Errorf("chat: bump: %w", err)
		}
		side := string(w.side)
		m := dbgen.ChatMessage{
			ChatID: id, Seq: seq, Kind: KindText, AuthorSide: &side, AuthorUserID: &user.ID, ToSide: string(to),
			Body: text, EventData: []byte("{}"), ClientID: &clientID, CreatedAt: now,
		}
		if err := q.InsertChatMessage(ctx, dbgen.InsertChatMessageParams{
			ChatID: id, Seq: seq, Kind: KindText, AuthorSide: m.AuthorSide, AuthorUserID: m.AuthorUserID, ToSide: m.ToSide,
			Body: text, EventData: m.EventData, ClientID: m.ClientID, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("chat: insert message: %w", err)
		}
		if err := q.UnarchiveChat(ctx, dbgen.UnarchiveChatParams{ChatID: id, Now: now}); err != nil {
			return fmt.Errorf("chat: unarchive: %w", err)
		}
		out, err = s.one(ctx, q, row, w, user, m)
		return err
	})
	return out, created, err
}

// ownMessage находит сообщение и проверяет, что его вправе менять этот человек и пока можно. Порядок проверок:
// есть ли оно, не строка ли о событии, своё ли; про закрытие и срок решает вызывающий.
func (s *Service) ownMessage(ctx context.Context, q *dbgen.Queries, user auth.User, id uuid.UUID, seq int) (dbgen.ChatMessage, error) {
	m, err := q.GetChatMessage(ctx, dbgen.GetChatMessageParams{ChatID: id, Seq: num.Int32(seq)})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.ChatMessage{}, ErrMessageNotFound
	}
	if err != nil {
		return dbgen.ChatMessage{}, fmt.Errorf("chat: load message: %w", err)
	}
	if m.Kind == KindSystem {
		return dbgen.ChatMessage{}, ErrSystemMessage
	}
	if m.AuthorUserID == nil || *m.AuthorUserID != user.ID {
		return dbgen.ChatMessage{}, ErrNotAuthor
	}
	return m, nil
}

// Edit исправляет своё сообщение в течение суток, пока переписка открыта.
func (s *Service) Edit(ctx context.Context, user auth.User, id uuid.UUID, seq int, in EditInput) (Message, error) {
	text, problem := cleanText(in.Text)
	if problem != "" {
		if _, _, err := s.load(ctx, s.q, user, id); err != nil {
			return Message{}, err
		}
		return Message{}, &auth.ValidationError{Fields: map[string]string{"text": problem}}
	}
	var out Message
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, w, err := s.load(ctx, q, user, id)
		if err != nil {
			return err
		}
		st, err := s.lockState(ctx, q, id, row.ApplicationID, row.OfferID)
		if err != nil {
			return err
		}
		m, err := s.ownMessage(ctx, q, user, id, seq)
		if err != nil {
			return err
		}
		if m.DeletedAt != nil {
			return ErrMessageNotFound
		}
		if st.Closed {
			return ErrClosed
		}
		now := s.now()
		if now.After(m.CreatedAt.Add(EditWindow)) {
			return ErrEditWindowClosed
		}
		if text != m.Body {
			if err := q.EditChatMessage(ctx, dbgen.EditChatMessageParams{Body: text, Now: now, ChatID: id, Seq: m.Seq}); err != nil {
				return fmt.Errorf("chat: edit message: %w", err)
			}
			m.Body, m.EditedAt = text, &now
		}
		out, err = s.one(ctx, q, row, w, user, m)
		return err
	})
	return out, err
}

// Delete удаляет своё сообщение в течение суток, пока переписка открыта. Текст стирается, на месте остаётся пометка.
// Повторное удаление ничего не меняет.
func (s *Service) Delete(ctx context.Context, user auth.User, id uuid.UUID, seq int) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		row, _, err := s.load(ctx, q, user, id)
		if err != nil {
			return err
		}
		st, err := s.lockState(ctx, q, id, row.ApplicationID, row.OfferID)
		if err != nil {
			return err
		}
		m, err := s.ownMessage(ctx, q, user, id, seq)
		if err != nil {
			return err
		}
		if m.DeletedAt != nil {
			return nil
		}
		if st.Closed {
			return ErrClosed
		}
		now := s.now()
		if now.After(m.CreatedAt.Add(EditWindow)) {
			return ErrEditWindowClosed
		}
		if err := q.DeleteChatMessage(ctx, dbgen.DeleteChatMessageParams{Now: now, ChatID: id, Seq: m.Seq}); err != nil {
			return fmt.Errorf("chat: delete message: %w", err)
		}
		return nil
	})
}

// MarkRead отмечает прочитанным всё до номера upTo включительно. Отметка только растёт и не уходит дальше последнего
// сообщения. У стороны организации «прочитано» значит, что прочитал хоть один сотрудник.
func (s *Service) MarkRead(ctx context.Context, user auth.User, id uuid.UUID, upTo int) error {
	if upTo < 0 {
		return &BadParamError{Param: "up_to"}
	}
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		_, w, err := s.load(ctx, q, user, id)
		if err != nil {
			return err
		}
		// Сначала строка переписки, потом личная отметка: так же идёт отправка (замок переписки, затем возврат из архива).
		// В обратном порядке чтение и отправка в архивную переписку ждали бы друг друга.
		if err := q.RaiseSideRead(ctx, dbgen.RaiseSideReadParams{Side: string(w.side), UpTo: num.Int32(upTo), ID: id}); err != nil {
			return fmt.Errorf("chat: raise side read: %w", err)
		}
		if err := q.MarkChatRead(ctx, dbgen.MarkChatReadParams{ChatID: id, UserID: user.ID, UpTo: num.Int32(upTo), Now: s.now()}); err != nil {
			return fmt.Errorf("chat: mark read: %w", err)
		}
		return nil
	})
}

// Archive кладёт переписку в личный архив человека или возвращает из него. У собеседника и коллег она остаётся там, где была.
func (s *Service) Archive(ctx context.Context, user auth.User, id uuid.UUID, archived bool) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		if _, _, err := s.load(ctx, q, user, id); err != nil {
			return err
		}
		if err := q.SetChatArchived(ctx, dbgen.SetChatArchivedParams{ChatID: id, UserID: user.ID, Archived: archived, Now: s.now()}); err != nil {
			return fmt.Errorf("chat: set archived: %w", err)
		}
		return nil
	})
}

// Typing проверяет, что человеку можно печатать в этой переписке. Рассылка «печатает» собеседнику появится вместе с живым
// потоком (срез 39), здесь только права и закрытие.
func (s *Service) Typing(ctx context.Context, user auth.User, id uuid.UUID) error {
	row, _, err := s.load(ctx, s.q, user, id)
	if err != nil {
		return err
	}
	if StateOf(row.ApplicationStatus, row.OfferStatus, row.Blocked).Closed {
		return ErrClosed
	}
	return nil
}

// Check проверяет, что человек участник переписки: чужой и несуществующей она «не существует».
func (s *Service) Check(ctx context.Context, user auth.User, id uuid.UUID) error {
	_, _, err := s.load(ctx, s.q, user, id)
	return err
}
