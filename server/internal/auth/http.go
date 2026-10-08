package auth

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
	"scibox/server/internal/erasure"
	"scibox/server/internal/httpguard"
)

// CookieName — имя cookie с сессией.
const CookieName = "scibox_session"

// DeviceCookieName — долгая cookie знакомого браузера (письмо о входе с нового устройства, T-13).
const DeviceCookieName = "scibox_device"

// maxBody — самый большой допустимый размер тела запроса аккаунтов.
const maxBody = 16 << 10

// Коды ошибок аккаунтов.
const (
	CodeInvalidCredentials = "invalid_credentials" //nolint:gosec // G101: код ошибки для сайта, а не пароль
	CodeEmailNotConfirmed  = "email_not_confirmed"
	CodeInvalidToken       = "invalid_token"
	CodeUnsupportedMedia   = apierr.CodeUnsupportedMedia
	CodeEmailTaken         = "email_taken"
	CodeDeletionBlocked    = "deletion_blocked"
)

// Handler — HTTP-часть аккаунтов: cookie, проверка источника запроса, обработчики.
type Handler struct {
	svc        *Service
	logger     *slog.Logger
	secure     bool
	publicHost string
}

// NewHandler собирает обработчики. Cookie помечается Secure, если сайт открывается по https.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	u, _ := url.Parse(svc.cfg.PublicURL)
	h := &Handler{svc: svc, logger: logger}
	if u != nil {
		h.secure = u.Scheme == "https"
		h.publicHost = u.Host
	}
	return h
}

type ctxKey struct{}

// FromContext возвращает вошедшего человека, если он есть. Работает после Authenticate.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Mount подключает адреса /auth/* и /account/* к роутеру, который уже живёт под /api.
// Перед ним роутер должен подключить SameOrigin и Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Get("/me", h.me)
		r.Post("/register", h.register)
		r.Post("/confirm-email", h.confirmEmail)
		r.Post("/resend-confirmation", h.resendConfirmation)
		r.Post("/login", h.login)
		r.Post("/logout", h.logout)
		r.Post("/forgot-password", h.forgotPassword)
		r.Post("/reset-password", h.resetPassword)
		r.Post("/confirm-email-change", h.confirmEmailChange)
		r.Post("/revert-email-change", h.revertEmailChange)
	})
	r.Route("/account", func(r chi.Router) {
		r.Use(h.RequireUser)
		r.Patch("/", h.updateAccount)
		r.Post("/password", h.changePassword)
		r.Post("/reauth", h.reauth)
		r.Get("/sessions", h.listSessions)
		r.Delete("/sessions/{id}", h.revokeSession)
		r.Post("/sessions/revoke-others", h.revokeOthers)
		r.Post("/email", h.requestEmailChange)
		r.Get("/deletion", h.deletionPlan)
		r.Post("/deletion", h.requestDeletion)
		r.Post("/deletion/cancel", h.cancelDeletion)
	})
}

// ---- промежуточные обработчики ----

// SameOrigin отклоняет запросы, меняющие данные, если они пришли с чужого сайта.
// Вместе с SameSite=Lax у cookie это защита от подделки запросов (CSRF).
func (h *Handler) SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Запрос не из браузера или из очень старого. Современный браузер сообщает источник в Sec-Fetch-Site.
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Запрос пришёл с другого сайта")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || (u.Host != r.Host && u.Host != h.publicHost) {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Запрос пришёл с другого сайта")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Authenticate кладёт вошедшего человека в контекст запроса. Без cookie или с устаревшей сессией запрос идёт дальше как анонимный.
func (h *Handler) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		p, err := h.svc.Authenticate(r.Context(), c.Value)
		switch {
		case err == nil:
			if p.Refreshed {
				h.setCookie(w, c.Value, p.ExpiresAt)
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
		case errors.Is(err, ErrUnauthenticated):
			h.clearCookie(w)
			next.ServeHTTP(w, r)
		default:
			h.fail(w, r, err)
		}
	})
}

// RequireUser пропускает только вошедших.
func (h *Handler) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "Войдите в аккаунт")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- вспомогательное ----

func (h *Handler) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	// Secure зависит от адреса сайта (https), проверено TestHTTPCookieIsSecureOverHTTPS.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure ставится при https, gosec не видит значение поля
		Name: CookieName, Value: token, Path: "/", Expires: expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

// setDeviceCookie запоминает браузер как знакомый на DeviceTTL. Пустой токен — менять нечего.
func (h *Handler) setDeviceCookie(w http.ResponseWriter, token string) {
	if token == "" {
		return
	}
	ttl := h.svc.cfg.DeviceTTL
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure ставится при https, gosec не видит значение поля
		Name: DeviceCookieName, Value: token, Path: "/", Expires: time.Now().Add(ttl), MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure ставится при https, gosec не видит значение поля
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

// meta — адрес и браузер человека. Адрес с учётом доверенных прокси решает периметр (httpguard, T-06):
// без доверенных прокси это адрес соединения, заголовки X-Forwarded-* не принимаются.
func meta(r *http.Request) Meta {
	m := Meta{IP: httpguard.ClientIP(r), UserAgent: r.UserAgent()}
	if c, err := r.Cookie(DeviceCookieName); err == nil {
		m.Device = c.Value
	}
	return m
}

// decode читает JSON-тело запроса; при ошибке сам отвечает и возвращает false.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	return apierr.DecodeJSON(w, r, dst, maxBody)
}

// fail превращает ошибку сервиса в ответ API.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		verr *ValidationError
		rerr *RateLimitedError
		berr *erasure.BlockedError
	)
	switch {
	case errors.As(err, &verr):
		apierr.WriteFieldErrors(w, "Проверьте поля формы", verr.Fields)
	case errors.Is(err, ErrReauthRequired):
		WriteReauthRequired(w)
	case errors.As(err, &berr):
		apierr.WriteError(w, http.StatusConflict, CodeDeletionBlocked,
			"Вы единственный владелец организации, где есть другие сотрудники. Сначала сделайте владельцем кого-то из них")
	case errors.Is(err, ErrEmailTaken):
		apierr.WriteError(w, http.StatusConflict, CodeEmailTaken, "Эта почта уже привязана к другому аккаунту")
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такой сессии нет")
	case errors.As(err, &rerr):
		secs := int(math.Ceil(rerr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: apierr.CodeRateLimited, Message: "Слишком много попыток. Подождите немного и попробуйте снова", RetryAfter: secs,
		}})
	case errors.Is(err, ErrInvalidCredentials):
		apierr.WriteError(w, http.StatusUnauthorized, CodeInvalidCredentials, "Почта или пароль указаны неверно")
	case errors.Is(err, ErrEmailNotConfirmed):
		apierr.WriteError(w, http.StatusForbidden, CodeEmailNotConfirmed, "Почта ещё не подтверждена. Откройте ссылку из письма или запросите письмо заново")
	case errors.Is(err, ErrInvalidToken):
		apierr.WriteError(w, http.StatusBadRequest, CodeInvalidToken, "Ссылка устарела или уже использована. Запросите новую")
	case errors.Is(err, ErrUnauthenticated):
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "Войдите в аккаунт")
	default:
		apierr.WriteInternal(w, r, h.logger, "auth", err)
	}
}

// WriteReauthRequired отвечает 403 reauth_required: сайт покажет окно пароля и повторит запрос.
// Нужен и другим разделам (назначение владельца организации).
func WriteReauthRequired(w http.ResponseWriter) {
	apierr.WriteError(w, http.StatusForbidden, apierr.CodeReauthRequired, "Для этого действия введите пароль ещё раз")
}

type userJSON struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	EmailConfirmed bool      `json:"email_confirmed"`
	CreatedAt      time.Time `json:"created_at"`
	// DeletionDueAt — когда аккаунт будет удалён (null — удаление не назначено).
	DeletionDueAt *time.Time `json:"deletion_due_at"`
}

func toJSON(u User) userJSON {
	return userJSON{
		ID: u.ID.String(), Email: u.Email, Name: u.Name, EmailConfirmed: u.EmailConfirmed, CreatedAt: u.CreatedAt,
		DeletionDueAt: u.DeletionDueAt,
	}
}

type userResponse struct {
	User *userJSON `json:"user"`
}

func writeUser(w http.ResponseWriter, status int, u User) {
	j := toJSON(u)
	apierr.WriteJSON(w, status, userResponse{User: &j})
}

// ---- обработчики ----

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	p, ok := FromContext(r.Context())
	if !ok {
		apierr.WriteJSON(w, http.StatusOK, userResponse{})
		return
	}
	writeUser(w, http.StatusOK, p.User)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Consent  bool   `json:"consent"`
	}
	if !decode(w, r, &in) {
		return
	}
	email, err := h.svc.Register(r.Context(), RegisterInput{Name: in.Name, Email: in.Email, Password: in.Password, Consent: in.Consent}, meta(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusAccepted, map[string]string{"email": email})
}

func (h *Handler) confirmEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	user, sess, err := h.svc.ConfirmEmail(r.Context(), in.Token, meta(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.setCookie(w, sess.Token, sess.ExpiresAt)
	h.setDeviceCookie(w, sess.Device)
	writeUser(w, http.StatusOK, user)
}

// emailOnly читает тело {"email": "..."} и вызывает действие; ответ всегда 202.
func (h *Handler) emailOnly(action func(*Service, context.Context, string, Meta) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := action(h.svc, r.Context(), in.Email, meta(r)); err != nil {
			h.fail(w, r, err)
			return
		}
		apierr.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
	}
}

func (h *Handler) resendConfirmation(w http.ResponseWriter, r *http.Request) {
	h.emailOnly((*Service).ResendConfirmation)(w, r)
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	h.emailOnly((*Service).RequestPasswordReset)(w, r)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	user, sess, err := h.svc.Login(r.Context(), in.Email, in.Password, meta(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Вход всегда даёт новую сессию; прежняя сессия этого браузера (любого человека) завершается (T-10).
	if c, err := r.Cookie(CookieName); err == nil {
		if err := h.svc.Logout(r.Context(), c.Value); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.setCookie(w, sess.Token, sess.ExpiresAt)
	h.setDeviceCookie(w, sess.Device)
	writeUser(w, http.StatusOK, user)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		if err := h.svc.Logout(r.Context(), c.Value); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.svc.ResetPassword(r.Context(), in.Token, in.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	h.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, _ := FromContext(r.Context())
	user, err := h.svc.UpdateName(r.Context(), p, in.Name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeUser(w, http.StatusOK, user)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, _ := FromContext(r.Context())
	if err := h.svc.ChangePassword(r.Context(), p, in.CurrentPassword, in.NewPassword); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) revokeOthers(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	if err := h.svc.RevokeOtherSessions(r.Context(), p); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- срез 20: свежий пароль, сессии, смена почты, удаление ----

func (h *Handler) reauth(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, _ := FromContext(r.Context())
	if err := h.svc.Reauth(r.Context(), p, in.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionJSON struct {
	ID         string    `json:"id"`
	Browser    string    `json:"browser"`
	OS         string    `json:"os"`
	Mobile     bool      `json:"mobile"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	list, err := h.svc.ListSessions(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]sessionJSON, len(list))
	for i, s := range list {
		out[i] = sessionJSON{
			ID: s.ID.String(), Browser: s.Device.Browser, OS: s.Device.OS, Mobile: s.Device.Mobile,
			CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, Current: s.Current,
		}
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, ErrNotFound)
		return
	}
	p, _ := FromContext(r.Context())
	if err := h.svc.RevokeSession(r.Context(), p, id); err != nil {
		h.fail(w, r, err)
		return
	}
	if id == p.SessionID {
		h.clearCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requestEmailChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, _ := FromContext(r.Context())
	if err := h.svc.RequestEmailChange(r.Context(), p, in.Email, meta(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}

func (h *Handler) confirmEmailChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	user, err := h.svc.ConfirmEmailChange(r.Context(), in.Token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeUser(w, http.StatusOK, user)
}

func (h *Handler) revertEmailChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.svc.RevertEmailChange(r.Context(), in.Token); err != nil {
		h.fail(w, r, err)
		return
	}
	// Все сессии завершены, в том числе, может быть, эта.
	h.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type orgRefJSON struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func orgRefs(orgs []erasure.Org) []orgRefJSON {
	out := make([]orgRefJSON, len(orgs))
	for i, o := range orgs {
		out[i] = orgRefJSON{Slug: o.Slug, Name: o.Name}
	}
	return out
}

func (h *Handler) deletionPlan(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	plan, err := h.svc.DeletionPlan(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{
		"due_at":     p.User.DeletionDueAt,
		"grace_days": int(h.svc.cfg.DeletionGrace / (24 * time.Hour)),
		"blocking":   orgRefs(plan.Blocking),
		"orphaned":   orgRefs(plan.Orphaned),
	})
}

func (h *Handler) requestDeletion(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	due, err := h.svc.RequestDeletion(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Все сессии завершены, в том числе эта: человек вышел из аккаунта.
	h.clearCookie(w)
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"due_at": due})
}

func (h *Handler) cancelDeletion(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	if err := h.svc.CancelDeletion(r.Context(), p); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
