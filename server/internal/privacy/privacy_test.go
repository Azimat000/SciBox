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

// Каждая пара «кто смотрит × режим» записана явно: и профиль, и контакты.
func TestWhoSeesWhat(t *testing.T) {
	anon := Viewer{}
	signedIn := Viewer{} // вошедший без организации выглядит для приватности так же, как аноним
	staff := Viewer{Staff: true}
	owner := Viewer{Owner: true}
	ownerAndStaff := Viewer{Owner: true, Staff: true}

	tests := []struct {
		name          string
		mode          Visibility
		who           Viewer
		view, contact bool
	}{
		{"аноним, скрыт", Hidden, anon, false, false},
		{"аноним, организациям", Orgs, anon, false, false},
		{"аноним, публичный", Public, anon, true, false},

		{"вошедший без организации, скрыт", Hidden, signedIn, false, false},
		{"вошедший без организации, организациям", Orgs, signedIn, false, false},
		{"вошедший без организации, публичный", Public, signedIn, true, false},

		{"сотрудник организации, скрыт", Hidden, staff, false, false},
		{"сотрудник организации, организациям", Orgs, staff, true, true},
		{"сотрудник организации, публичный", Public, staff, true, true},

		{"владелец, скрыт", Hidden, owner, true, true},
		{"владелец, организациям", Orgs, owner, true, true},
		{"владелец, публичный", Public, owner, true, true},
		{"владелец и сотрудник, скрыт", Hidden, ownerAndStaff, true, true},

		{"неизвестный режим закрыт для аноним", Visibility(""), anon, false, false},
		{"неизвестный режим закрыт для сотрудника", Visibility("friends"), staff, false, false},
		{"неизвестный режим открыт владельцу", Visibility("friends"), owner, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mode.CanView(tc.who); got != tc.view {
				t.Errorf("CanView = %v, ожидали %v", got, tc.view)
			}
			if got := tc.mode.CanSeeContacts(tc.who); got != tc.contact {
				t.Errorf("CanSeeContacts = %v, ожидали %v", got, tc.contact)
			}
		})
	}
}
