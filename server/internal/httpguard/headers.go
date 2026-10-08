package httpguard

import (
	"net/http"
	"strings"
)

// Политики содержимого (CSP, T-05). Сайт — собранное приложение Vite: скрипты, стили, шрифты и картинки
// лежат на том же адресе, встроенных скриптов в index.html нет, поэтому 'unsafe-inline' не нужен ни скриптам,
// ни стилям (React меняет style через свойства элемента, CSP это не запрещает). Картинки data: — значки в CSS.
// Ответам API никакое содержимое не нужно: JSON браузер не исполняет, а файлы отклика дополнительно
// закрыты песочницей (internal/files).
const (
	siteCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
		"connect-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	apiCSP = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

	// hsts — браузер год ходит на сайт только по https. Без includeSubDomains: под тем же доменом могут жить
	// чужие поддомены без https (у туннеля tuna так и есть).
	hsts = "max-age=31536000"

	// permissionsPolicy — сайту не нужны камера, микрофон, место, оплата и прочие возможности устройства.
	permissionsPolicy = "accelerometer=(), browsing-topics=(), camera=(), display-capture=(), geolocation=(), " +
		"gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), usb=()"
)

// IsAPIPath — адрес API (/api и всё под ним); остальное — страницы и файлы сайта.
func IsAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// setHeaders ставит заголовки защиты до обработчика: он может заменить их своими (выдача файлов ставит
// CSP-песочницу), но забыть их не может.
func setHeaders(h http.Header, path string, https bool) {
	if IsAPIPath(path) {
		h.Set("Content-Security-Policy", apiCSP)
	} else {
		csp := siteCSP
		if https {
			csp += "; upgrade-insecure-requests"
		}
		h.Set("Content-Security-Policy", csp)
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY") // для старых браузеров, которые не знают frame-ancestors
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", permissionsPolicy)
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	// HSTS только для сайта на https: по http браузер этот заголовок всё равно не принимает,
	// а локальный сайт на localhost им не испортить.
	if https {
		h.Set("Strict-Transport-Security", hsts)
	}
}
