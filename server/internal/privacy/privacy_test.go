package privacy

import "testing"

func TestParse(t *testing.T) {
	for _, m := range Modes {
		got, ok := Parse(string(m))
		if !ok || got != m {
			t.Errorf("Parse(%q) = %q, %v", m, got, ok)
		}
	}
	for _, bad := range []string{"", "Public", "private", "orgs ", "all"} {
		if got, ok := Parse(bad); ok || got != "" {
			t.Errorf("Parse(%q) = %q, %v, ожидали отказ", bad, got, ok)
		}
	}
}

// Каждая тройка «кто смотрит × режим × скрыт ли от него и раскрыто ли ему имя» записана явно: уровень раскрытия,
// страница и контакты. Таблица не вычисляется тем же кодом, что проверяет.
func TestWhoSeesWhat(t *testing.T) {
	viewers := []struct {
		name string
		who  Viewer
	}{
		{"аноним", Viewer{}},
		{"сотрудник", Viewer{Staff: true}},
		{"сотрудник организации, от которой скрыто", Viewer{Staff: true, Blocked: true}},
		{"сотрудник организации, которой открыто имя", Viewer{Staff: true, Revealed: true}},
		{"сотрудник двух организаций: от одной скрыто, другой открыто имя", Viewer{Staff: true, Blocked: true, Revealed: true}},
		{"владелец", Viewer{Owner: true}},
		{"владелец и сотрудник организации, от которой скрыто", Viewer{Owner: true, Staff: true, Blocked: true}},
		{"вошедший без организации с чужими отметками", Viewer{Blocked: true, Revealed: true}},
		{"вошедший без организации, имя открыто", Viewer{Revealed: true}},
	}
	C, M, O := Closed, Masked, Open
	// Порядок столбцов совпадает с порядком viewers.
	modes := []struct {
		mode Visibility
		want []Disclosure
	}{
		{Hidden, []Disclosure{C, C, C, C, C, O, O, C, C}},
		{Anonymous, []Disclosure{C, M, C, O, C, O, O, C, C}},
		{Orgs, []Disclosure{C, O, C, O, C, O, O, C, C}},
		{Public, []Disclosure{O, O, C, O, C, O, O, O, O}},
		{Visibility(""), []Disclosure{C, C, C, C, C, O, O, C, C}},
		{Visibility("friends"), []Disclosure{C, C, C, C, C, O, O, C, C}},
	}
	for _, m := range modes {
		for i, v := range viewers {
			t.Run(string(m.mode)+"/"+v.name, func(t *testing.T) {
				got := m.mode.Disclose(v.who)
				if got != m.want[i] {
					t.Fatalf("Disclose = %v, ожидали %v", got, m.want[i])
				}
				if view := m.want[i] != C; m.mode.CanView(v.who) != view {
					t.Errorf("CanView = %v, ожидали %v", !view, view)
				}
				// Контакты: только при полном раскрытии и только владельцу или сотруднику организации.
				contacts := m.want[i] == O && (v.who.Owner || v.who.Staff)
				if got := m.mode.CanSeeContacts(v.who); got != contacts {
					t.Errorf("CanSeeContacts = %v, ожидали %v", got, contacts)
				}
			})
		}
	}
}

// Анонимная карточка не открывает ни профиль целиком, ни контакты: «видит карточку» не равно «видит человека».
func TestMaskedIsNeverOpen(t *testing.T) {
	who := Viewer{Staff: true}
	if Anonymous.Disclose(who) != Masked {
		t.Fatal("сотрудник должен видеть анонимную карточку")
	}
	if Anonymous.CanSeeContacts(who) {
		t.Error("контакты анонимного профиля закрыты, пока имя не открыто")
	}
}

// Какие режимы видит каталог: каждый смотрящий явно. Владелец в каталоге ничем не отличается от остальных:
// собственный профиль в каталог не попадает, поэтому его права на «скрытый» профиль здесь не работают.
func TestCatalogModes(t *testing.T) {
	tests := []struct {
		name string
		who  Viewer
		want []Visibility
	}{
		{"аноним", Viewer{}, []Visibility{Public}},
		{"вошедший без организации", Viewer{}, []Visibility{Public}},
		{"сотрудник организации", Viewer{Staff: true}, []Visibility{Anonymous, Orgs, Public}},
		// Скрытие и раскрытие зависят от самого профиля, их проверяет запрос по каждой строке (profiles.Catalog).
		{"сотрудник, от которого скрыто", Viewer{Staff: true, Blocked: true, Revealed: true}, []Visibility{Anonymous, Orgs, Public}},
		{"владелец", Viewer{Owner: true}, []Visibility{Public}},
		{"владелец и сотрудник", Viewer{Owner: true, Staff: true}, []Visibility{Anonymous, Orgs, Public}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CatalogModes(tc.who)
			if len(got) != len(tc.want) {
				t.Fatalf("CatalogModes = %v, ожидали %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("CatalogModes = %v, ожидали %v", got, tc.want)
				}
			}
			for _, m := range got {
				if m == Hidden {
					t.Errorf("скрытые профили не должны попадать в каталог")
				}
			}
		})
	}
}

func TestMaskedLabel(t *testing.T) {
	tests := []struct {
		name string
		in   MaskedFacts
		want string
	}{
		{"пример из дорожной карты", MaskedFacts{Degree: "candidate", Level: 2, HIndex: 9, Specialties: []string{"1.4.4"}}, "Кандидат наук, 1.4.4, R2, h-index 9"},
		{"доктор, три специальности: берутся две", MaskedFacts{Degree: "doctor", Level: 3, HIndex: 21, Specialties: []string{"1.4.4", "1.4.7", "1.5.2"}}, "Доктор наук, 1.4.4, 1.4.7, R3, h-index 21"},
		{"без степени, без h-index и специальностей", MaskedFacts{Degree: "none", Level: 1}, "Без учёной степени, R1"},
		{"неизвестная степень и уровень", MaskedFacts{Degree: "x", Level: 9}, "Без учёной степени"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskedLabel(tc.in); got != tc.want {
				t.Errorf("MaskedLabel = %q, ожидали %q", got, tc.want)
			}
		})
	}
}
