package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// Device — браузер и система, как их можно понять по строке User-Agent. Значения — коды:
// подписи для людей у сайта (ru.ts) и в письмах (Label).
type Device struct {
	Browser string // chrome, firefox, safari, edge, opera, yandex, samsung или ""
	OS      string // windows, macos, ios, android, linux, chromeos или ""
	Mobile  bool
}

// ParseUserAgent разбирает строку User-Agent. Строку присылает браузер, и подделать её легко, поэтому
// результат — только подсказка человеку («похоже, это Chrome на Windows»), а не доказательство.
func ParseUserAgent(ua string) Device {
	var d Device
	// Порядок важен: Edge, Opera, Яндекс и Samsung пишут о себе ещё и «Chrome» и «Safari».
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgA/"), strings.Contains(ua, "EdgiOS/"):
		d.Browser = "edge"
	case strings.Contains(ua, "YaBrowser/"), strings.Contains(ua, "YaSearchBrowser/"):
		d.Browser = "yandex"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		d.Browser = "opera"
	case strings.Contains(ua, "SamsungBrowser/"):
		d.Browser = "samsung"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		d.Browser = "firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS/"), strings.Contains(ua, "Chromium/"):
		d.Browser = "chrome"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		d.Browser = "safari"
	}
	switch {
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "iPod"):
		d.OS = "ios"
	case strings.Contains(ua, "Android"):
		d.OS = "android"
	case strings.Contains(ua, "Windows"):
		d.OS = "windows"
	case strings.Contains(ua, "CrOS"):
		d.OS = "chromeos"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		d.OS = "macos"
	case strings.Contains(ua, "Linux"):
		d.OS = "linux"
	}
	d.Mobile = strings.Contains(ua, "Mobi") || strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPod")
	return d
}

var (
	browserNames = map[string]string{
		"chrome": "Chrome", "firefox": "Firefox", "safari": "Safari", "edge": "Edge",
		"opera": "Opera", "yandex": "Яндекс Браузер", "samsung": "Samsung Internet",
	}
	osNames = map[string]string{
		"windows": "Windows", "macos": "macOS", "ios": "iOS", "android": "Android", "linux": "Linux", "chromeos": "ChromeOS",
	}
)

// Label — подпись для письма: «Chrome, Windows», «Safari, iOS», «неизвестный браузер».
func (d Device) Label() string {
	b, o := browserNames[d.Browser], osNames[d.OS]
	switch {
	case b != "" && o != "":
		return b + ", " + o
	case b != "":
		return b
	case o != "":
		return "браузер на " + o
	}
	return "неизвестный браузер"
}

// maxDeviceToken — длиннее токен из cookie не бывает (newToken даёт 43 знака): чужую строку не храним.
const maxDeviceToken = 64

// rememberDevice отмечает браузер знакомым для человека. raw — токен из cookie устройства (может быть пустым).
// Возвращает токен для cookie (новый, если браузер его не прислал) и isNew — с этого браузера человек
// раньше не входил.
func (s *Service) rememberDevice(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, raw string) (token string, isNew bool, err error) {
	if raw == "" || len(raw) > maxDeviceToken {
		raw, _ = newToken()
	}
	now := s.now()
	hash := hashToken(raw)
	n, err := q.TouchKnownDevice(ctx, dbgen.TouchKnownDeviceParams{UserID: userID, DeviceHash: hash, LastSeenAt: now})
	if err != nil {
		return "", false, fmt.Errorf("auth: touch device: %w", err)
	}
	if n > 0 {
		return raw, false, nil
	}
	if err := q.AddKnownDevice(ctx, dbgen.AddKnownDeviceParams{UserID: userID, DeviceHash: hash, FirstSeenAt: now}); err != nil {
		return "", false, fmt.Errorf("auth: add device: %w", err)
	}
	return raw, true, nil
}
