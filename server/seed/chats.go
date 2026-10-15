package seed

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/chat"
)

// Демо-переписки (срез 38). Строки о событиях строятся по итоговому состоянию откликов и приглашений тем же переносом, что
// и для прежних записей (chat.Backfill): статусы, приглашения и решения демо-откликов ставятся в базе напрямую, минуя
// события. Поверх этого в часть переписок вплетаются сообщения людей, по времени между строками о событиях, чтобы была
// переписка, закрытая отказом (сообщения до решения), переписка с анонимным учёным и переписка с непрочитанным.

// demoSpeaker — кто пишет: учёный или сотрудник организации вакансии.
type demoSpeaker int

const (
	byScientist demoSpeaker = iota
	byStaff
)

type demoText struct {
	speaker demoSpeaker
	// afterHours — через сколько часов после начала переписки (первой строки о событии) написано сообщение.
	afterHours float64
	text       string
}

// demoChat — переписка, в которую вплетаются сообщения. Выбор по учёному: отклик под номером application (по порядку
// создания) или приглашение на вакансию (application < 0).
type demoChat struct {
	scientist   string
	application int
	// unread — сколько последних сообщений остаются непрочитанными адресатом, остальные прочитаны всеми.
	unread int
	texts  []demoText
}

var demoChats = []demoChat{
	// Приглашение на собеседование и контакты ждут ответа; последнее слово за организацией (непрочитанное у учёного).
	{"korolev", 2, 1, []demoText{
		{byStaff, 1, "Дмитрий, здравствуйте! Спасибо за отклик. Мы отправили приглашение на собеседование, посмотрите, пожалуйста."},
		{byScientist, 3, "Здравствуйте! Приглашение получил, время подходит. Подскажите, можно ли присоединиться онлайн?"},
		{byStaff, 5, "Да, ссылка на встречу в приглашении. Если понадобится, пришлём напоминание за день."},
	}},
	// Отказ: сообщения идут до решения, переписка закрыта, история остаётся.
	{"korolev", 3, 0, []demoText{
		{byStaff, 2, "Добрый день! Уточните, пожалуйста, есть ли у вас опыт работы на синхротроне."},
		{byScientist, 6, "Здравствуйте! Да, два года на источнике синхротронного излучения, подробности в резюме."},
	}},
	// Принят: разговор после решения продолжается.
	{"korolev", 4, 0, []demoText{
		{byStaff, 50, "Дмитрий, поздравляем! Оформление пришлём отдельным письмом, а пока скажите, когда вам удобно выйти."},
		{byScientist, 52, "Спасибо! Готов выйти с первого числа."},
	}},
	// Предложенное время и оставленные контакты.
	{"guseva", 0, 0, []demoText{
		{byStaff, 4, "Надежда, мы увидели, что 20-е вам не подходит. Посмотрим четверг или пятницу после обеда."},
		{byScientist, 8, "Спасибо, пятница после трёх подойдёт."},
	}},
	// Приглашение на вакансию анонимному учёному: организация видит «Анонимный соискатель», пока он не откроется.
	{"lebedeva", -1, 1, []demoText{
		{byScientist, 2, "Здравствуйте! Расскажите, пожалуйста, подробнее, какие пробы нужно будет обрабатывать."},
		{byStaff, 5, "Здравствуйте! Керны донных отложений из южной котловины, около 40 штук за сезон. Пришлём описание методики."},
		{byScientist, 9, "Спасибо, посмотрю и отвечу на приглашение."},
	}},
	// Ответ «Интересно»: имя открыто, идёт разговор.
	{"guseva", -1, 0, []demoText{
		{byStaff, 3, "Надежда, спасибо за ответ! Ждём ваш отклик на вакансию, при вопросах пишите сюда."},
		{byScientist, 7, "Конечно, отправлю отклик сегодня вечером."},
	}},
}

// seedChats строит демо-переписки. Повторный запуск ничего не меняет: когда в переписках уже есть тексты людей, пропуск.
func seedChats(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID, now time.Time) (int, error) {
	var texts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chat_messages WHERE kind = 'text'`).Scan(&texts); err != nil {
		return 0, fmt.Errorf("seed chats: %w", err)
	}
	if texts > 0 {
		return 0, nil
	}
	// До этого шага переписки заводили только настоящие вызовы откликов и приглашений: одна строка у каждой. Строки по
	// итоговому состоянию строятся заново.
	if _, err := pool.Exec(ctx, `DELETE FROM chats`); err != nil {
		return 0, fmt.Errorf("seed chats: reset: %w", err)
	}
	if _, err := chat.Backfill(ctx, pool); err != nil {
		return 0, fmt.Errorf("seed chats: %w", err)
	}
	n := 0
	for i, dc := range demoChats {
		if err := weave(ctx, pool, ids, dc, i, now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

type storedMessage struct {
	kind, toSide, body, eventType string
	authorSide                    *string
	authorUser                    *uuid.UUID
	data                          []byte
	at                            time.Time
}

// weave вплетает сообщения людей в переписку по времени и перенумеровывает её; отметки прочтения ставятся заново.
func weave(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID, dc demoChat, index int, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("seed chats: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user := ids[dc.scientist]
	var chatID, orgID uuid.UUID
	if dc.application >= 0 {
		err = tx.QueryRow(ctx, `SELECT c.id, c.org_id FROM chats c JOIN applications a ON a.id = c.application_id
			WHERE a.user_id = $1 ORDER BY a.created_at, a.id OFFSET $2 LIMIT 1`, user, dc.application).Scan(&chatID, &orgID)
	} else {
		err = tx.QueryRow(ctx, `SELECT c.id, c.org_id FROM chats c WHERE c.user_id = $1 AND c.offer_id IS NOT NULL
			ORDER BY c.created_at, c.id LIMIT 1`, user).Scan(&chatID, &orgID)
	}
	if err != nil {
		return fmt.Errorf("seed chats: chat of %s #%d: %w", dc.scientist, dc.application, err)
	}
	var staff uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 ORDER BY (role = 'owner') DESC, joined_at, user_id LIMIT 1`, orgID).Scan(&staff); err != nil {
		return fmt.Errorf("seed chats: staff of the organization: %w", err)
	}

	rows, err := tx.Query(ctx, `SELECT kind, to_side, body, event_type, author_side, author_user_id, event_data, created_at
		FROM chat_messages WHERE chat_id = $1 ORDER BY seq`, chatID)
	if err != nil {
		return fmt.Errorf("seed chats: read: %w", err)
	}
	msgs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (storedMessage, error) {
		var m storedMessage
		err := r.Scan(&m.kind, &m.toSide, &m.body, &m.eventType, &m.authorSide, &m.authorUser, &m.data, &m.at)
		return m, err
	})
	if err != nil {
		return fmt.Errorf("seed chats: read: %w", err)
	}
	start := msgs[0].at
	for _, t := range dc.texts {
		side, author, to := "scientist", user, "org"
		if t.speaker == byStaff {
			side, author, to = "org", staff, "scientist"
		}
		at := start.Add(time.Duration(t.afterHours * float64(time.Hour)))
		msgs = append(msgs, storedMessage{kind: chat.KindText, toSide: to, body: t.text, authorSide: &side, authorUser: &author, data: []byte("{}"), at: at})
	}
	slices.SortStableFunc(msgs, func(a, b storedMessage) int { return a.at.Compare(b.at) })
	// Статусы демо-откликов поставлены «на два дня позже отклика», то есть в будущем: переписка сдвигается назад целиком,
	// так, чтобы последнее сообщение было не позже «сейчас».
	if end := msgs[len(msgs)-1].at; end.After(now) {
		shift := end.Sub(now) + time.Duration(10+3*index)*time.Minute
		for i := range msgs {
			msgs[i].at = msgs[i].at.Add(-shift)
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM chat_messages WHERE chat_id = $1`, chatID); err != nil {
		return fmt.Errorf("seed chats: clear: %w", err)
	}
	var lastScientist, lastOrg int
	for i, m := range msgs {
		seq := i + 1
		if _, err := tx.Exec(ctx, `INSERT INTO chat_messages (chat_id, seq, kind, author_side, author_user_id, to_side, body, event_type, event_data, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, chatID, seq, m.kind, m.authorSide, m.authorUser, m.toSide, m.body, m.eventType, m.data, m.at); err != nil {
			return fmt.Errorf("seed chats: insert: %w", err)
		}
		if m.toSide == "scientist" {
			lastScientist = seq
		} else {
			lastOrg = seq
		}
	}
	last := len(msgs)
	// Непрочитанными остаются последние dc.unread сообщений: они адресованы той стороне, которой писали, и её отметка стоит
	// перед ними. Остальное прочитано обеими сторонами.
	readSeq := last
	if dc.unread > 0 {
		readSeq = max(last-dc.unread, 0)
	}
	scientistRead, orgRead := last, last
	if dc.unread > 0 {
		if msgs[last-1].toSide == "scientist" {
			scientistRead = readSeq
		} else {
			orgRead = readSeq
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE chats SET last_seq = $2, last_for_scientist = $3, last_for_org = $4, scientist_read = $5, org_read = $6,
		updated_at = $7, created_at = LEAST(created_at, $8) WHERE id = $1`, chatID, last, lastScientist, lastOrg, scientistRead, orgRead, msgs[last-1].at, msgs[0].at); err != nil {
		return fmt.Errorf("seed chats: counters: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_participants WHERE chat_id = $1`, chatID); err != nil {
		return fmt.Errorf("seed chats: marks: %w", err)
	}
	// Отметки людей: учёный и каждый, кто ведёт отклики, прочитали до своей стороны.
	if _, err := tx.Exec(ctx, `INSERT INTO chat_participants (chat_id, user_id, read_seq, updated_at) VALUES ($1, $2, $3, $4)`, chatID, user, scientistRead, now); err != nil {
		return fmt.Errorf("seed chats: marks: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_participants (chat_id, user_id, read_seq, updated_at)
		SELECT $1, m.user_id, $3, $4 FROM org_members m WHERE m.org_id = $2 AND m.user_id <> $5
		ON CONFLICT (chat_id, user_id) DO NOTHING`, chatID, orgID, orgRead, now, user); err != nil {
		return fmt.Errorf("seed chats: marks: %w", err)
	}
	return tx.Commit(ctx)
}
