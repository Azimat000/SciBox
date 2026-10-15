package httpapi

import (
	"net/http"
	"time"

	"scibox/server/internal/auth"
	"scibox/server/internal/httpguard"
)

// Perimeter — настройки периметра сервера (срез 19).
type Perimeter struct {
	Proxies httpguard.Proxies
	HTTPS   bool
	// Store — где считать запросы; nil — в памяти процесса (одна копия сервера, тесты).
	Store httpguard.Store
	// Now — часы для ограничения частоты; nil — time.Now.
	Now func() time.Time
}

// Политики частоты (T-07, D-141). Гость считается по IP: за одним адресом университета может сидеть много людей,
// поэтому гостевой предел выше. Вошедший считается по аккаунту: человеку больше двух запросов в секунду не нужно.
var (
	// publicReads — поиск, каталоги, главная, страницы вакансий, людей и организаций, справочник журналов.
	publicReads = &httpguard.Policy{
		Name:  "public_read",
		Guest: httpguard.Rate{Requests: 300, Per: time.Minute},
		User:  httpguard.Rate{Requests: 120, Per: time.Minute},
	}
	// skillSuggest — подсказки навыков в форме профиля: запрос на каждую паузу в наборе (до четырёх в секунду у
	// быстрого человека, два поля), поэтому выше общего предела чтения; гостю подсказки не отдаются, его предел мал
	// (срез 33).
	skillSuggest = &httpguard.Policy{
		Name:  "skill_suggest",
		Guest: httpguard.Rate{Requests: 30, Per: time.Minute},
		User:  httpguard.Rate{Requests: 240, Per: time.Minute},
	}
	// pdfBuilds — резюме PDF собирается на каждый запрос: это самое тяжёлое чтение.
	pdfBuilds = &httpguard.Policy{
		Name:  "pdf_build",
		Guest: httpguard.Rate{Requests: 20, Per: time.Minute},
		User:  httpguard.Rate{Requests: 20, Per: time.Minute},
	}
	// applyUploads — отправка отклика с файлами: каждый запрос гонит до 30 МБ через хранилище и антивирус и собирает PDF.
	// Человеку хватит пары в минуту; предел не даёт одной учётной записи нагружать их циклом (срез 29, CON-3).
	applyUploads = &httpguard.Policy{
		Name:  "apply_upload",
		Guest: httpguard.Rate{Requests: 10, Per: time.Minute},
		User:  httpguard.Rate{Requests: 10, Per: time.Minute},
	}
	// exportFiles — выгрузка откликов в файл: до пяти тысяч строк и запись в журнал за один запрос. Человеку хватит нескольких
	// в минуту; предел не даёт скриптом вытягивать отклики организации по кругу (срез 16, T-87).
	exportFiles = &httpguard.Policy{
		Name:  "export_file",
		Guest: httpguard.Rate{Requests: 10, Per: time.Minute},
		User:  httpguard.Rate{Requests: 10, Per: time.Minute},
	}
	// chatReads — список переписок, шапка, лента, число в шапке (срез 37). Окно переписки открывает несколько запросов
	// сразу, список обновляется при событиях; человеку больше четырёх в секунду не нужно.
	chatReads = &httpguard.Policy{
		Name:  "chat_read",
		Guest: httpguard.Rate{Requests: 30, Per: time.Minute},
		User:  httpguard.Rate{Requests: 240, Per: time.Minute},
	}
	// chatStreams — открытие живого потока чата (срез 39): сайт держит один поток и переподключается раз в 4 минуты или
	// после обрыва с нарастающей паузой; предел не даёт зациклившейся вкладке открывать соединения без счёта. Число одновременных
	// потоков ограничивает сам поток (на человека и на копию, D-244).
	chatStreams = &httpguard.Policy{
		Name:  "chat_stream",
		Guest: httpguard.Rate{Requests: 5, Per: time.Minute},
		User:  httpguard.Rate{Requests: 30, Per: time.Minute},
	}
	// chatSends — отправка сообщения: живой человек пишет не чаще пары сообщений в минуту подряд; 30 в минуту дают запас
	// на быстрый диалог и не дают засыпать собеседника потоком (T-99, предел уточняется в срезе 40).
	chatSends = &httpguard.Policy{
		Name:  "chat_send",
		Guest: httpguard.Rate{Requests: 10, Per: time.Minute},
		User:  httpguard.Rate{Requests: 30, Per: time.Minute},
	}
	// chatSignals — мелкие действия в переписке: «прочитано», «печатает», архив, правка и удаление своего сообщения.
	chatSignals = &httpguard.Policy{
		Name:  "chat_signal",
		Guest: httpguard.Rate{Requests: 10, Per: time.Minute},
		User:  httpguard.Rate{Requests: 180, Per: time.Minute},
	}
	// chatComplaints — жалобы на организацию в переписке (срез 38): человеку хватит нескольких в час; предел не даёт
	// засыпать модерацию (срез 30). Общий предел по человеку и организации ужесточит срез 40 (T-99).
	chatComplaints = &httpguard.Policy{
		Name:  "chat_complaint",
		Guest: httpguard.Rate{Requests: 5, Per: time.Hour},
		User:  httpguard.Rate{Requests: 10, Per: time.Hour},
	}
	// linkPages — страницы по одноразовым ссылкам из писем (рекомендатель, приглашение в организацию,
	// смена почты): человеку хватит нескольких запросов, перебор ссылок должен упираться в предел.
	linkPages = &httpguard.Policy{
		Name:  "link_page",
		Guest: httpguard.Rate{Requests: 30, Per: time.Minute},
		User:  httpguard.Rate{Requests: 30, Per: time.Minute},
	}
)

// uploadBody — лимит тела загрузки с файлами. Точный предел ставит сам обработчик (internal/files):
// до 5 файлов по 10 МБ и данные формы; здесь запас сверху, чтобы чужой огромный запрос не читался вовсе.
const uploadBody = 64 << 20

// PerimeterRoutes — адреса с особыми правилами. Остальные получают лимит тела httpguard.DefaultBody, обычные сроки
// и без ограничения частоты. Каждый шаблон проверяется по настоящему роутеру (cli: TestPerimeter).
func PerimeterRoutes() []httpguard.Route {
	get, post := http.MethodGet, http.MethodPost
	patch, del := http.MethodPatch, http.MethodDelete
	return []httpguard.Route{
		{Method: get, Pattern: "/api/health", Limit: publicReads},
		{Method: get, Pattern: "/api/landing", Limit: publicReads},
		{Method: get, Pattern: "/api/vacancies", Limit: publicReads},
		{Method: get, Pattern: "/api/vacancies/{id}/", Limit: publicReads},
		{Method: get, Pattern: "/api/scientists", Limit: publicReads},
		{Method: get, Pattern: "/api/scientists/{id}", Limit: publicReads},
		{Method: get, Pattern: "/api/organizations/", Limit: publicReads},
		{Method: get, Pattern: "/api/organizations/{slug}/", Limit: publicReads},
		{Method: get, Pattern: "/api/organizations/{slug}/units/{unitID}/", Limit: publicReads},
		{Method: get, Pattern: "/api/journals", Limit: publicReads},
		{Method: get, Pattern: "/api/skills", Limit: skillSuggest},
		{Method: get, Pattern: "/api/profile/doi", Limit: publicReads},

		{Method: get, Pattern: "/api/profile/cv", Slow: true, Limit: pdfBuilds},
		{Method: get, Pattern: "/api/scientists/{id}/cv", Slow: true, Limit: pdfBuilds},
		{Method: get, Pattern: "/api/applications/{id}/files/{fileId}", Slow: true},
		{Method: get, Pattern: "/api/account/export", Slow: true},
		{Method: get, Pattern: "/api/my/candidates/export", Limit: exportFiles},
		{Method: post, Pattern: "/api/applications", Body: uploadBody, Slow: true, Limit: applyUploads},

		{Method: get, Pattern: "/api/chats", Limit: chatReads},
		{Method: get, Pattern: "/api/chats/events", Limit: chatStreams},
		{Method: get, Pattern: "/api/chats/unread-count", Limit: chatReads},
		{Method: get, Pattern: "/api/chats/vacancies", Limit: chatReads},
		{Method: get, Pattern: "/api/chats/{id}", Limit: chatReads},
		{Method: get, Pattern: "/api/chats/{id}/messages", Limit: chatReads},
		{Method: post, Pattern: "/api/chats/{id}/messages", Limit: chatSends},
		{Method: patch, Pattern: "/api/chats/{id}/messages/{seq}", Limit: chatSignals},
		{Method: del, Pattern: "/api/chats/{id}/messages/{seq}", Limit: chatSignals},
		{Method: post, Pattern: "/api/chats/{id}/read", Limit: chatSignals},
		{Method: post, Pattern: "/api/chats/{id}/archive", Limit: chatSignals},
		{Method: post, Pattern: "/api/chats/{id}/unarchive", Limit: chatSignals},
		{Method: post, Pattern: "/api/chats/{id}/typing", Limit: chatSignals},
		{Method: post, Pattern: "/api/chats/{id}/block", Limit: chatSignals},
		{Method: post, Pattern: "/api/chats/{id}/complaints", Limit: chatComplaints},
		{Method: get, Pattern: "/api/chat-blocks", Limit: chatReads},
		{Method: del, Pattern: "/api/chat-blocks/{orgId}", Limit: chatSignals},
		{Method: get, Pattern: "/api/orgs/{orgId}/chat-templates", Limit: chatReads},
		{Method: post, Pattern: "/api/orgs/{orgId}/chat-templates", Limit: chatSignals},
		{Method: patch, Pattern: "/api/orgs/{orgId}/chat-templates/{id}", Limit: chatSignals},
		{Method: del, Pattern: "/api/orgs/{orgId}/chat-templates/{id}", Limit: chatSignals},

		{Method: post, Pattern: "/api/recommendations/lookup", Limit: linkPages},
		{Method: post, Pattern: "/api/recommendations/submit", Body: uploadBody, Slow: true, Limit: linkPages},
		{Method: post, Pattern: "/api/recommendations/decline", Limit: linkPages},
		{Method: post, Pattern: "/api/invitations/lookup", Limit: linkPages},
		{Method: post, Pattern: "/api/auth/confirm-email-change", Limit: linkPages},
		{Method: post, Pattern: "/api/auth/revert-email-change", Limit: linkPages},
	}
}

// whoIsSignedIn — номер вошедшего для ограничения частоты (после auth.Authenticate).
func whoIsSignedIn(r *http.Request) (string, bool) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		return "", false
	}
	return p.User.ID.String(), true
}
