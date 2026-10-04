package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// WebHandler отдаёт собранный сайт (web/dist) с того же адреса, что и API (D-131).
// Так сайт и сервер помещаются на один порт: это нужно туннелю и серверу в интернете.
// Адреса без файла (/vacancies/42, /profile) получают index.html: страницы рисует сам сайт.
// Файлы из /assets/ с хешем в имени кешируются надолго, index.html не кешируется,
// чтобы после обновления сайта браузер сразу взял новую версию.
func WebHandler(site fs.FS) http.Handler {
	files := http.FileServerFS(site)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "Этот метод здесь не поддерживается")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(site, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			// Пропавший файл сборки (старая вкладка после обновления) — честный 404, а не страница вместо скрипта.
			if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(site, "index.html")
		if err != nil {
			http.Error(w, "site is not built", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}
