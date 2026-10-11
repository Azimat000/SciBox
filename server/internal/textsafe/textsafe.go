// Package textsafe проверяет текст, пришедший от человека, на то, чего в тексте быть не может: нулевой знак и
// невалидный UTF-8. PostgreSQL отвергает и то, и другое (SQLSTATE 22021), и без проверки такой ввод давал 500 на публичных
// адресах, а сохранённый поиск с таким значением вставал в начало очереди рассылки и блокировал её (срез 29, INJ-1…INJ-3).
package textsafe

import (
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Valid — строка годится для базы: валидный UTF-8 и ни одного нулевого знака.
func Valid(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// ValidQuery проверяет разобранные параметры адреса.
func ValidQuery(q url.Values) bool {
	for k, vs := range q {
		if !Valid(k) {
			return false
		}
		for _, v := range vs {
			if !Valid(v) {
				return false
			}
		}
	}
	return true
}

// ValidRequestURL проверяет путь и параметры запроса так, как их увидит обработчик (после раскодирования).
func ValidRequestURL(u *url.URL) bool {
	if !Valid(u.Path) {
		return false
	}
	if u.RawQuery == "" {
		return true
	}
	// Неразбираемая строка запроса здесь не ошибка: параметры, которые разобрать не удалось, обработчик не увидит.
	q, _ := url.ParseQuery(u.RawQuery)
	return ValidQuery(q)
}

// ValidValue обходит значение (структуры, срезы, словари, указатели, интерфейсы) и проверяет каждую строку в нём.
// Нужна для тела запроса после разбора JSON: в нём нулевой знак записывается как \u0000 и разбирается без ошибки.
func ValidValue(v any) bool {
	return walk(reflect.ValueOf(v), 0)
}

// maxDepth защищает от самоссылающихся значений; настоящие тела запросов мельче.
const maxDepth = 64

func walk(v reflect.Value, depth int) bool {
	if depth > maxDepth {
		return false
	}
	switch v.Kind() {
	case reflect.String:
		return Valid(v.String())
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return true
		}
		return walk(v.Elem(), depth+1)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return true // []byte, например json.RawMessage: не текст
		}
		for i := range v.Len() {
			if !walk(v.Index(i), depth+1) {
				return false
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if !walk(it.Key(), depth+1) || !walk(it.Value(), depth+1) {
				return false
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() && !walk(v.Field(i), depth+1) {
				return false
			}
		}
	default:
	}
	return true
}
