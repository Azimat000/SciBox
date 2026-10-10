package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// secretVars — настройки с секретами, которые можно передать файлом: вместо `X=значение` задаётся `X_FILE=/путь`
// (срез 28, T-27). Значение в окружении процесса видно в `docker inspect`, в `/proc/<pid>/environ` и во всех отчётах
// о запуске, файл с правами 600 (docker compose `secrets:`) — нет.
var secretVars = []string{
	"DATABASE_URL",
	"SCIBOX_S3_ACCESS_KEY",
	"SCIBOX_S3_SECRET_KEY",
	"SCIBOX_SMTP_PASSWORD",
	"SCIBOX_OPS_TOKEN",
	"SCIBOX_ERROR_DSN",
}

// maxSecretFile — самый большой файл с секретом: пароль, ключ или адрес занимают килобайты, а не мегабайты.
// Предел защищает от указанного по ошибке большого файла или устройства.
const maxSecretFile = 64 << 10

// withSecretFiles возвращает getenv, который для каждой переменной из secretVars отдаёт содержимое файла из
// `<ИМЯ>_FILE`. Правила, при которых запуск останавливается с понятной ошибкой, а не работает с пустым или чужим
// секретом:
//   - заданы и `X`, и `X_FILE`: непонятно, какой из них настоящий;
//   - файла нет, это не обычный файл, он пуст или больше maxSecretFile;
//   - в боевом режиме (strict) файл доступен группе или всем: `chmod 600`.
//
// Один перевод строки в конце файла отбрасывается (так пишет `echo` и редакторы), остальные пробелы остаются: они
// могут быть частью пароля. В текст ошибок попадает имя переменной и путь, содержимое файла никогда.
func withSecretFiles(getenv func(string) string, strict bool) (func(string) string, error) {
	values := map[string]string{}
	var problems []error
	for _, name := range secretVars {
		path := strings.TrimSpace(getenv(name + "_FILE"))
		if path == "" {
			continue
		}
		if getenv(name) != "" {
			problems = append(problems, fmt.Errorf("%s and %s_FILE are both set: keep only one", name, name))
			continue
		}
		v, err := readSecretFile(path, strict)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s_FILE: %w", name, err))
			continue
		}
		values[name] = v
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return func(key string) string {
		if v, ok := values[key]; ok {
			return v
		}
		return getenv(key)
	}, nil
}

func readSecretFile(path string, strict bool) (string, error) {
	f, err := os.Open(path) //nolint:gosec // G304: путь из настройки администратора (X_FILE), а не от посетителя
	if err != nil {
		return "", err // *PathError: путь и причина, содержимого нет
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("cannot stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if strict && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is readable by other users (mode %04o): run chmod 600 on it", path, info.Mode().Perm())
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSecretFile+1))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	if len(raw) > maxSecretFile {
		return "", fmt.Errorf("%s is larger than %d bytes: a secret file holds one value", path, maxSecretFile)
	}
	v := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return v, nil
}
