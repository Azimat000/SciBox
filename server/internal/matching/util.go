package matching

import (
	"net/url"
	"sort"
)

// parseValues разбирает строку запроса. Ошибка — строка записана неверно (например, «%zz»).
func parseValues(raw string) (url.Values, error) { return url.ParseQuery(raw) }

// firstMessage — сообщение об ошибке первого по алфавиту поля: человеку достаточно одного.
func firstMessage(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fields[keys[0]]
}

// sortStable сортирует срез, сохраняя порядок равных.
func sortStable[T any](items []T, less func(a, b T) bool) {
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
}
