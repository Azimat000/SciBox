// Package httpapi содержит HTTP-обработчики и маршруты API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"scibox/server/internal/auth"
	"scibox/server/internal/health"
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
