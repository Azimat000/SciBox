// Package httpapi содержит HTTP-обработчики и маршруты API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"scibox/server/internal/applications"
	"scibox/server/internal/auth"
	"scibox/server/internal/health"
	"scibox/server/internal/journals"
	"scibox/server/internal/landing"
	"scibox/server/internal/matching"
	"scibox/server/internal/notifications"
	"scibox/server/internal/offers"
	"scibox/server/internal/orgs"
	"scibox/server/internal/profiles"
	"scibox/server/internal/refdata"
	"scibox/server/internal/references"
	"scibox/server/internal/vacancies"
)

// HealthChecker проверяет базу данных.
type HealthChecker interface {
	CheckDatabase(ctx context.Context) (health.Database, error)
}

// Deps — всё, что нужно обработчикам.
type Deps struct {
	ProductName string
	Version     string
	Health      HealthChecker
	Logger      *slog.Logger
	// Auth подключает аккаунты (/api/auth, /api/account); без него этих адресов нет.
	Auth *auth.Handler
	// Orgs подключает организации и подразделения (/api/organizations, /api/my, /api/invitations). Нужен вместе с Auth.
	Orgs *orgs.Handler
	// Vacancies подключает вакансии (/api/vacancies, /api/my/vacancies). Нужен вместе с Auth.
	Vacancies *vacancies.Handler
	// Profiles подключает профили учёных (/api/profile, /api/scientists). Нужен вместе с Auth.
	Profiles *profiles.Handler
	// Applications подключает отклики (/api/applications). Нужен вместе с Auth.
	Applications *applications.Handler
	// References подключает рекомендательные письма (/api/applications/{id}/references, /api/recommendations). Нужен вместе с Auth.
	References *references.Handler
	// Notifications подключает уведомления (/api/notifications). Нужен вместе с Auth.
	Notifications *notifications.Handler
	// Offers подключает приглашения учёных на вакансии (/api/offers, /api/my/sent-offers). Нужен вместе с Auth.
	Offers *offers.Handler
	// Matching подключает избранное, сохранённые поиски, подбор и сроки (/api/favorites, /api/saved-searches, /api/matches,
	// /api/deadlines). Нужен вместе с Auth.
	Matching *matching.Handler
	// Reference подключает справочники (/api/reference).
	Reference *refdata.Handler
	// Landing подключает числа для главной страницы (/api/landing, публично).
	Landing *landing.Handler
	// Journals подключает поиск по справочнику журналов (/api/journals). Нужен вместе с Auth.
	Journals *journals.Handler
}

// healthTimeout ограничивает проверку базы, чтобы /api/health не зависал.
const healthTimeout = 2 * time.Second

// NewRouter собирает маршруты API.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Logger))
	r.Use(recoverer(d.Logger))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "Такого адреса нет")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "Этот метод здесь не поддерживается")
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(noStore)
		if d.Auth != nil {
			r.Use(d.Auth.SameOrigin, d.Auth.Authenticate)
			d.Auth.Mount(r)
		}
		if d.Orgs != nil {
			d.Orgs.Mount(r)
		}
		if d.Vacancies != nil {
			d.Vacancies.Mount(r)
		}
		if d.Profiles != nil {
			d.Profiles.Mount(r)
		}
		if d.Applications != nil {
			d.Applications.Mount(r)
		}
		if d.References != nil {
			d.References.Mount(r)
		}
		if d.Notifications != nil {
			d.Notifications.Mount(r)
		}
		if d.Offers != nil {
			d.Offers.Mount(r)
		}
		if d.Matching != nil {
			d.Matching.Mount(r)
		}
		if d.Reference != nil {
			d.Reference.Mount(r)
		}
		if d.Landing != nil {
			d.Landing.Mount(r)
		}
		if d.Journals != nil {
			d.Journals.Mount(r)
		}
		r.Get("/health", healthHandler(d))
	})
	return r
}

// HealthResponse — ответ /api/health, когда всё в порядке.
type HealthResponse struct {
	Status   string          `json:"status"`
	Product  string          `json:"product"`
	Version  string          `json:"version"`
	Database health.Database `json:"database"`
}

func healthHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()
		db, err := d.Health.CheckDatabase(ctx)
		if err != nil {
			d.Logger.Warn("health check failed", "err", err)
			WriteError(w, http.StatusServiceUnavailable, CodeDatabaseUnavailable, "База данных недоступна")
			return
		}
		WriteJSON(w, http.StatusOK, HealthResponse{
			Status:   "ok",
			Product:  d.ProductName,
			Version:  d.Version,
			Database: db,
		})
	}
}

// noStore запрещает браузеру и посредникам кешировать ответы API: в них личные данные.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.Error("panic in handler", "panic", rec, "path", r.URL.Path)
					WriteError(w, http.StatusInternalServerError, CodeInternal, "Что-то сломалось на сервере. Попробуйте ещё раз")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
