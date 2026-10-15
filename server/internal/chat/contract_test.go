package chat

// Сверка ответов сервера с contracts/chat/*.json «по полям и типам» (contracts/chat/README.md). Алгоритм тот же, что у
// сайта (web/src/test/contract.ts): из всех примеров файла собирается форма; ответ подходит, если у каждого значения тип
// из набора, нет лишних и пропущенных обязательных полей, строки по путям enums взяты из перечня, а вид объекта
// (variants) известен.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

type contractExample struct {
	Request *struct {
		Query string `json:"query"`
		Body  any    `json:"body"`
	} `json:"request"`
	Status   int `json:"status"`
	Response any `json:"response"`
}

type contractFile struct {
	Endpoint     string                     `json:"endpoint"`
	Enums        map[string]string          `json:"enums"`
	RequestEnums map[string]string          `json:"request_enums"`
	Variants     map[string]string          `json:"variants"`
	Maps         []string                   `json:"maps"`
	Errors       []string                   `json:"errors"`
	Examples     map[string]contractExample `json:"examples"`
}

const contractDir = "../../../contracts/chat"

func loadContract(t *testing.T, name string) contractFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractDir, name+".json")) //nolint:gosec // G304: каталог контракта и имя файла заданы в самом тесте
	if err != nil {
		t.Fatal(err)
	}
	var f contractFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

var contractEnums = func() map[string][]string {
	raw, err := os.ReadFile(filepath.Join(contractDir, "enums.json"))
	if err != nil {
		panic(err)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		panic(err)
	}
	out := map[string][]string{}
	for k, v := range all {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		for _, e := range list {
			out[k] = append(out[k], e.(string))
		}
	}
	return out
}()

type cField struct {
	shape    *cShape
	required bool
}

type cShape struct {
	types    map[string]bool
	keys     map[string]cField
	items    *cShape
	values   *cShape
	variants *cVariants
}

type cVariants struct {
	key     string
	byValue map[string]*cShape
}

func typeOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	}
	return fmt.Sprintf("%T", v)
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

type cRules struct {
	variants map[string]string
	maps     []string
}

func buildShape(values []any, path string, rules cRules, skipVariant bool) *cShape {
	s := &cShape{types: map[string]bool{}}
	var objects []map[string]any
	for _, v := range values {
		s.types[typeOf(v)] = true
		if o, ok := v.(map[string]any); ok {
			objects = append(objects, o)
		}
	}
	if key, ok := rules.variants[path]; ok && !skipVariant && len(objects) > 0 {
		groups := map[string][]any{}
		for _, o := range objects {
			k := fmt.Sprint(o[key])
			groups[k] = append(groups[k], o)
		}
		s.variants = &cVariants{key: key, byValue: map[string]*cShape{}}
		for k, g := range groups {
			s.variants.byValue[k] = buildShape(g, path, rules, true)
		}
		return s
	}
	switch {
	case len(objects) > 0 && slices.Contains(rules.maps, path):
		var all []any
		for _, o := range objects {
			for _, v := range o {
				all = append(all, v)
			}
		}
		s.values = buildShape(all, path+".*", rules, false)
	case len(objects) > 0:
		s.keys = map[string]cField{}
		names := map[string]bool{}
		for _, o := range objects {
			for k := range o {
				names[k] = true
			}
		}
		for name := range names {
			var present []any
			for _, o := range objects {
				if v, ok := o[name]; ok {
					present = append(present, v)
				}
			}
			s.keys[name] = cField{shape: buildShape(present, joinPath(path, name), rules, false), required: len(present) == len(objects)}
		}
	}
	var elements []any
	for _, v := range values {
		if a, ok := v.([]any); ok {
			elements = append(elements, a...)
		}
	}
	if len(elements) > 0 {
		s.items = buildShape(elements, path+"[]", rules, false)
	}
	return s
}

func checkShape(value any, s *cShape, path string, enumPaths map[string]string, errs *[]string) {
	where := path
	if where == "" {
		where = "(root)"
	}
	t := typeOf(value)
	if !s.types[t] {
		var want []string
		for k := range s.types {
			want = append(want, k)
		}
		sort.Strings(want)
		*errs = append(*errs, fmt.Sprintf("%s: type %s, want %s", where, t, strings.Join(want, " | ")))
		return
	}
	if name, ok := enumPaths[path]; ok {
		if str, ok := value.(string); ok {
			var allowed []string
			for _, n := range strings.Split(name, "|") {
				allowed = append(allowed, contractEnums[n]...)
			}
			if !slices.Contains(allowed, str) {
				*errs = append(*errs, fmt.Sprintf("%s: value %q is not in %s", where, str, name))
			}
		}
	}
	obj, isObj := value.(map[string]any)
	if s.variants != nil && isObj {
		kind := fmt.Sprint(obj[s.variants.key])
		v, ok := s.variants.byValue[kind]
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: unknown %s=%q", where, s.variants.key, kind))
		} else {
			checkShape(value, v, path, enumPaths, errs)
		}
		return
	}
	if s.values != nil && isObj {
		for _, v := range obj {
			checkShape(v, s.values, path+".*", enumPaths, errs)
		}
	}
	if s.keys != nil && isObj {
		for name, f := range s.keys {
			if _, ok := obj[name]; f.required && !ok {
				*errs = append(*errs, fmt.Sprintf("%s: missing field %s", where, name))
			}
		}
		for name, v := range obj {
			f, ok := s.keys[name]
			if !ok {
				*errs = append(*errs, fmt.Sprintf("%s: extra field %s", where, name))
				continue
			}
			checkShape(v, f.shape, joinPath(path, name), enumPaths, errs)
		}
	}
	if arr, ok := value.([]any); ok && s.items != nil {
		for _, item := range arr {
			checkShape(item, s.items, path+"[]", enumPaths, errs)
		}
	}
}

func (f contractFile) responses() []any {
	var out []any
	for _, e := range f.Examples {
		if e.Response != nil {
			out = append(out, e.Response)
		}
	}
	return out
}

// responseErrors — что не так в ответе по сравнению с примерами файла.
func (f contractFile) responseErrors(raw string) []string {
	examples := f.responses()
	if len(examples) == 0 {
		if strings.TrimSpace(raw) != "" {
			return []string{"the endpoint has no response body"}
		}
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return []string{"not JSON: " + err.Error()}
	}
	var errs []string
	checkShape(v, buildShape(examples, "", cRules{variants: f.Variants, maps: f.Maps}, false), "", f.Enums, &errs)
	return errs
}

// requestErrors — что не так в теле запроса по сравнению с примерами файла.
func (f contractFile) requestErrors(body any) []string {
	var bodies []any
	for _, e := range f.Examples {
		if e.Request != nil && e.Request.Body != nil {
			bodies = append(bodies, e.Request.Body)
		}
	}
	if len(bodies) == 0 {
		return nil
	}
	var errs []string
	checkShape(body, buildShape(bodies, "", cRules{}, false), "", f.RequestEnums, &errs)
	return errs
}

// Сама сверка не должна быть слепой: она обязана видеть лишнее поле, пропущенное поле, чужой тип и значение вне перечня.
func TestContractCheckerSeesDifferences(t *testing.T) {
	f := loadContract(t, "unread-count")
	if errs := f.responseErrors(`{"total": 4, "scientist": 3, "org": 1}`); len(errs) != 0 {
		t.Errorf("a correct answer: %v", errs)
	}
	for name, raw := range map[string]string{
		"extra field":   `{"total": 4, "scientist": 3, "org": 1, "all": 5}`,
		"missing field": `{"total": 4, "scientist": 3}`,
		"wrong type":    `{"total": "4", "scientist": 3, "org": 1}`,
		"null":          `{"total": null, "scientist": 3, "org": 1}`,
		"not an object": `[1]`,
	} {
		if errs := f.responseErrors(raw); len(errs) == 0 {
			t.Errorf("%s: the difference is not seen", name)
		}
	}
	h := loadContract(t, "chat")
	bad := `{"chat": {"id": "x", "side": "admin"}}`
	if errs := h.responseErrors(bad); len(errs) == 0 {
		t.Error("an unknown enum value is not seen")
	}
	m := loadContract(t, "messages")
	if errs := m.responseErrors(`{"items": [{"id": "x", "seq": 1, "kind": "system", "author": null, "text": "", "event": {"type": "offer.created", "data": {"foo": 1}}, "created_at": "2026-10-14T09:40:00Z", "edited_at": null, "deleted": false, "editable_until": null, "client_id": null}], "has_more": false}`); len(errs) == 0 {
		t.Error("a wrong shape of an event is not seen")
	}
}
