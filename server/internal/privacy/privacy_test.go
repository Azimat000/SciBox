package privacy

import "testing"

func TestParse(t *testing.T) {
	for _, m := range Modes {
		got, ok := Parse(string(m))
		if !ok || got != m {
			t.Errorf("Parse(%q) = %q, %v", m, got, ok)
		}
	}
	for _, bad := range []string{"", "Public", "private", "orgs ", "all", "anonymous", "ANON"} {
		if got, ok := Parse(bad); ok || got != "" {
			t.Errorf("Parse(%q) = %q, %v, ожидали отказ", bad, got, ok)
		}
	}
}

// Каждая пара «кто смотрит × режим × скрыт ли от него × открыто ли ему имя» записана явно: профиль, контакты и подробность.
func TestWhoSeesWhat(t *testing.T) {
	var (
		anon          = Viewer{}
		signedIn      = Viewer{} // вошедший без организации выглядит для приватности так же, как аноним
		staff         = Viewer{Staff: true}
		staffRevealed = Viewer{Staff: true, Revealed: true}
		staffHidden   = Viewer{Staff: true, HiddenFrom: true}
		// Сотрудник двух организаций: в одной профиль скрыт, в другой имя открыто. Скрытость сильнее.
		staffHiddenAndRevealed = Viewer{Staff: true, HiddenFrom: true, Revealed: true}
		owner                  = Viewer{Owner: true}
		ownerAndStaff          = Viewer{Owner: true, Staff: true}
		// Владелец, скрывшийся от собственной организации, смотрит свой профиль: владельцу ничто не мешает.
		ownerHiddenFrom = Viewer{Owner: true, Staff: true, HiddenFrom: true}
	)

	tests := []struct {
		name          string
		mode          Visibility
		who           Viewer
		view, contact bool
		detail        Detail
	}{
		{"аноним, скрыт", Hidden, anon, false, false, Full},
		{"аноним, анонимный", Anonymous, anon, false, false, Blind},
		{"аноним, организациям", Orgs, anon, false, false, Full},
		{"аноним, публичный", Public, anon, true, false, Full},

		{"вошедший без организации, скрыт", Hidden, signedIn, false, false, Full},
		{"вошедший без организации, анонимный", Anonymous, signedIn, false, false, Blind},
		{"вошедший без организации, организациям", Orgs, signedIn, false, false, Full},
		{"вошедший без организации, публичный", Public, signedIn, true, false, Full},

		{"сотрудник организации, скрыт", Hidden, staff, false, false, Full},
		{"сотрудник организации, анонимный", Anonymous, staff, true, false, Blind},
		{"сотрудник организации, организациям", Orgs, staff, true, true, Full},
		{"сотрудник организации, публичный", Public, staff, true, true, Full},

		{"сотрудник, которому открыто имя, скрыт", Hidden, staffRevealed, false, false, Full},
		{"сотрудник, которому открыто имя, анонимный", Anonymous, staffRevealed, true, true, Full},
		{"сотрудник, которому открыто имя, организациям", Orgs, staffRevealed, true, true, Full},

		{"сотрудник скрытой организации, скрыт", Hidden, staffHidden, false, false, Full},
		{"сотрудник скрытой организации, анонимный", Anonymous, staffHidden, false, false, Blind},
		{"сотрудник скрытой организации, организациям", Orgs, staffHidden, false, false, Full},
		{"сотрудник скрытой организации, публичный", Public, staffHidden, false, false, Full},
		{"сотрудник скрытой организации и организации с открытым именем, анонимный", Anonymous, staffHiddenAndRevealed, false, false, Full},
		{"сотрудник скрытой организации и организации с открытым именем, публичный", Public, staffHiddenAndRevealed, false, false, Full},

		{"владелец, скрыт", Hidden, owner, true, true, Full},
		{"владелец, анонимный", Anonymous, owner, true, true, Full},
		{"владелец, организациям", Orgs, owner, true, true, Full},
		{"владелец, публичный", Public, owner, true, true, Full},
		{"владелец и сотрудник, скрыт", Hidden, ownerAndStaff, true, true, Full},
		{"владелец, скрывшийся от своей организации, анонимный", Anonymous, ownerHiddenFrom, true, true, Full},

		{"неизвестный режим закрыт для аноним", Visibility(""), anon, false, false, Full},
		{"неизвестный режим закрыт для сотрудника", Visibility("friends"), staff, false, false, Full},
		{"неизвестный режим открыт владельцу", Visibility("friends"), owner, true, true, Full},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mode.CanView(tc.who); got != tc.view {
				t.Errorf("CanView = %v, ожидали %v", got, tc.view)
			}
			if got := tc.mode.CanSeeContacts(tc.who); got != tc.contact {
				t.Errorf("CanSeeContacts = %v, ожидали %v", got, tc.contact)
			}
			if got := tc.mode.Detail(tc.who); got != tc.detail {
				t.Errorf("Detail = %v, ожидали %v", got, tc.detail)
			}
		})
	}
}

// Безымянность и контакты связаны: где подробность Blind, контактов нет ни у кого, кроме владельца (у него Full).
func TestBlindNeverShowsContacts(t *testing.T) {
	for _, mode := range append(append([]Visibility{}, Modes...), "", "friends") {
		for _, owner := range []bool{false, true} {
			for _, staff := range []bool{false, true} {
				for _, hidden := range []bool{false, true} {
					for _, revealed := range []bool{false, true} {
						who := Viewer{Owner: owner, Staff: staff, HiddenFrom: hidden, Revealed: revealed}
						if mode.Detail(who) == Blind && mode.CanSeeContacts(who) {
							t.Errorf("режим %q, смотрящий %+v: безымянная карточка с контактами", mode, who)
						}
						if hidden && !owner && mode.CanView(who) {
							t.Errorf("режим %q, смотрящий %+v: профиль виден сотруднику организации, от которой он скрыт", mode, who)
						}
					}
				}
			}
		}
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
		{"сотрудник, которому открыто чьё-то имя", Viewer{Staff: true, Revealed: true}, []Visibility{Anonymous, Orgs, Public}},
		{"владелец", Viewer{Owner: true}, []Visibility{Public}},
		{"владелец и сотрудник", Viewer{Owner: true, Staff: true}, []Visibility{Anonymous, Orgs, Public}},
		// «Скрыт от организации» зависит от профиля: запрос каталога отсекает такие профили отдельно, а режимы те же.
		{"сотрудник, от организации которого кто-то скрылся", Viewer{Staff: true, HiddenFrom: true}, []Visibility{Anonymous, Orgs, Public}},
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

func TestShowsName(t *testing.T) {
	tests := []struct {
		concealed, revealed, want bool
	}{
		{false, false, true}, // обычный профиль
		{false, true, true},  // открыт и так
		{true, false, false}, // анонимный или скрыт от организации, имя не открыто
		{true, true, true},   // откликнулся или ответил «Интересно»
	}
	for _, tc := range tests {
		if got := ShowsName(tc.concealed, tc.revealed); got != tc.want {
			t.Errorf("ShowsName(%v, %v) = %v, ожидали %v", tc.concealed, tc.revealed, got, tc.want)
		}
	}
}
