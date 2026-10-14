package skills

import (
	"errors"
	"strings"
	"testing"
)

const head = "kind;group;name;aliases\n"

func TestParse(t *testing.T) {
	src := "\ufeff" + head + "# комментарий\n\n" +
		"research;Методы;ПЦР;Полимеразная цепная реакция|PCR\n" +
		"general;Языки;Английский язык;\n" +
		"research;Данные;\"Python; R\";\n"
	p, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Skills) != 3 {
		t.Fatalf("skills = %+v", p.Skills)
	}
	if s := p.Skills[0]; s.Kind != KindResearch || s.Group != "Методы" || s.Name != "ПЦР" || len(s.Aliases) != 2 || s.Aliases[1] != "PCR" || s.Line != 4 {
		t.Fatalf("first = %+v", s)
	}
	if len(p.Skills[1].Aliases) != 0 || p.Skills[2].Name != "Python; R" {
		t.Fatalf("others = %+v", p.Skills[1:])
	}
}

func TestParseRefusals(t *testing.T) {
	long := strings.Repeat("я", MaxLen+1)
	for name, src := range map[string]string{
		"empty file":        "",
		"wrong header":      "kind;name;aliases\n",
		"header order":      "group;kind;name;aliases\n",
		"no rows":           head,
		"too few columns":   head + "research;Методы;ПЦР\n",
		"unknown kind":      head + "science;Методы;ПЦР;\n",
		"empty group":       head + "research;;ПЦР;\n",
		"empty name":        head + "research;Методы;;\n",
		"long name":         head + "research;Методы;" + long + ";\n",
		"long group":        head + "research;" + strings.Repeat("я", 61) + ";ПЦР;\n",
		"long alias":        head + "research;Методы;ПЦР;" + long + "\n",
		"extra spaces":      head + "research;Методы;ПЦР  в реальном времени;\n",
		"edge space":        head + "research;Методы; ПЦР;\n",
		"control character": head + "research;Методы;ПЦР\u0007;\n",
		"no letters":        head + "research;Методы;\u200b;\n",
		"invalid utf8":      head + "research;Методы;П\xffЦР;\n",
		"same name twice":   head + "research;Методы;ПЦР;\nresearch;Методы;пцр;\n",
		"alias is a name":   head + "research;Методы;ПЦР;\nresearch;Методы;Другое;пцр\n",
		"alias twice":       head + "research;Методы;ПЦР;A|a\n",
		"yo and ye":         head + "general;Учёт;Учёт;\ngeneral;Учёт;Учет;\n",
		"too many aliases":  head + "research;Методы;ПЦР;" + strings.Repeat("a|", maxAliases) + "b\n",
		"bad quote":         head + "research;Методы;\"ПЦР;\n",
	} {
		if _, err := Parse(strings.NewReader(src)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Один и тот же навык в двух видах — не повтор.
	if _, err := Parse(strings.NewReader(head + "research;Данные;Python;\ngeneral;Компьютер;Python;\n")); err != nil {
		t.Fatalf("same name in two kinds: %v", err)
	}
}

// Встроенный файл: правила качества, которые невозможно проверить глазами по 600 строкам.
func TestEmbeddedDictionary(t *testing.T) {
	p, err := Parse(strings.NewReader(string(embedded)))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	groups := map[string]int{}
	for _, s := range p.Skills {
		kinds[s.Kind]++
		groups[s.Kind+"/"+s.Group]++
		if Key(Key(s.Name)) != Key(s.Name) {
			t.Errorf("line %d: key of %q is not stable", s.Line, s.Name)
		}
		if strings.HasSuffix(s.Name, ".") || strings.HasSuffix(s.Name, ",") {
			t.Errorf("line %d: %q ends with punctuation", s.Line, s.Name)
		}
	}
	if kinds[KindResearch] < 300 || kinds[KindGeneral] < 150 {
		t.Fatalf("the dictionary is too small: %v", kinds)
	}
	// Группа из одного навыка почти всегда опечатка в названии группы.
	for g, n := range groups {
		if n < 3 {
			t.Errorf("group %q has only %d skills", g, n)
		}
	}
	// Те, на кого ссылается подсказка в справочнике подписей и примерах сайта, должны быть в справочнике.
	have := map[string]bool{}
	for _, s := range p.Skills {
		have[s.Kind+"/"+Key(s.Name)] = true
	}
	for _, want := range []string{"research/пцр", "research/python", "research/масс-спектрометрия", "general/microsoft excel", "general/английский язык", "general/публичные выступления"} {
		if !have[want] {
			t.Errorf("the dictionary has no %q", want)
		}
	}
}
