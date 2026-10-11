package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/privacy"
)

// orgOf — организация, в которой человек состоит (в тестах у сотрудника она одна).
func orgOf(t *testing.T, p person) (id uuid.UUID, slug string) {
	t.Helper()
	err := sharedPool.QueryRow(bg, `SELECT o.id, o.slug FROM org_members m JOIN organizations o ON o.id = m.org_id WHERE m.user_id = $1`, p.ID).Scan(&id, &slug)
	if err != nil {
		t.Fatalf("org of %s: %v", p.Name, err)
	}
	return id, slug
}

func reveal(t *testing.T, profile, org uuid.UUID) {
	t.Helper()
	if _, err := sharedPool.Exec(bg, `INSERT INTO profile_reveals (profile_id, org_id, reason, created_at) VALUES ($1, $2, 'application', now())`, profile, org); err != nil {
		t.Fatalf("reveal: %v", err)
	}
}

// Всё, по чему анонима можно узнать: ни одной из этих строк не должно быть в анонимных ответах.
var secrets = []string{
	"Анонимов Секретный", "Заведующий лабораторией Секретной", "Секретоград", "Секретный институт", "Лаборатория секретных катализаторов",
	"0000-0002-1825-0097", "1234-5678", "57190123456", "a-1234-2008", "secret.contact@example.ru", "Секретная тема диссертации",
	"Секретная публикация о катализе", "Секретный грант", "Новосибирск", "Новосибирская область", "Секретная методика", "ИК-спектроскопия",
}

func secretCore() CoreInput {
	in := goodCore()
	in.Headline = "Заведующий лабораторией Секретной"
	in.City = "Секретоград"
	in.About = "Лаборатория секретных катализаторов"
	in.DegreeInstitution = "Секретный институт"
	in.Dissertation = "Секретная тема диссертации"
	in.ContactEmail = "secret.contact@example.ru"
	in.HRsci, in.HScopus, in.HWos, in.HScholar = ptr(297), ptr(9), nil, nil // 297: метка, по которой тест находит себя в каталоге
	in.ResearchSkills = []string{"Секретная методика"}
	in.Specialties = []string{"1.4.4", "1.4.1"}
	return in
}

func noSecrets(t *testing.T, what string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if strings.Contains(string(raw), s) {
			t.Errorf("%s: в анонимном ответе есть %q:\n%s", what, s, raw)
		}
	}
}

// Каждая тройка «кто смотрит × режим × скрыт ли от него × раскрыто ли ему имя»: страница, каталог и резюме дают один ответ
// (D-199…D-205). Таблица записана явно и не считается кодом пакета privacy.
func TestAnonymousAndHiddenOrgsAcrossSurfaces(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Анонимов Секретный")
	stranger := w.user("Вошедший без организации")
	plain := w.staff("Сотрудник обычной организации")
	hiddenFrom := w.staff("Сотрудник скрытой организации")
	revealedTo := w.staff("Сотрудник организации с открытым именем")
	if _, err := w.svc.SaveCore(bg, owner.User, secretCore()); err != nil {
		t.Fatal(err)
	}
	w.addItem(owner, ItemInput{Kind: KindPublication, ItemFields: ItemFields{Title: "Секретная публикация о катализе", Authors: "Анонимов С.", PubType: "article", Year: ptr(2022)}})
	w.addItem(owner, ItemInput{Kind: KindGrant, ItemFields: ItemFields{Title: "Секретный грант", Funder: "РНФ", Role: "lead", YearFrom: ptr(2021)}})
	own, _ := w.svc.Own(bg, owner.User)
	id := own.Profile.ID
	hiddenOrg, hiddenSlug := orgOf(t, hiddenFrom)
	_ = hiddenOrg
	if _, err := w.svc.HideFrom(bg, owner.User, hiddenSlug); err != nil {
		t.Fatal(err)
	}
	revealOrg, _ := orgOf(t, revealedTo)
	reveal(t, id, revealOrg)

	C, M, O := privacy.Closed, privacy.Masked, privacy.Open
	people := []struct {
		name string
		p    *auth.User
		// Ответ в режимах hidden, anonymous, orgs, public.
		want [4]privacy.Disclosure
	}{
		{"аноним", nil, [4]privacy.Disclosure{C, C, C, O}},
		{"вошедший без организации", &stranger.User, [4]privacy.Disclosure{C, C, C, O}},
		{"сотрудник организации", &plain.User, [4]privacy.Disclosure{C, M, O, O}},
		{"сотрудник организации, от которой скрыто", &hiddenFrom.User, [4]privacy.Disclosure{C, C, C, C}},
		{"сотрудник организации, которой открыто имя", &revealedTo.User, [4]privacy.Disclosure{C, O, O, O}},
		{"владелец", &owner.User, [4]privacy.Disclosure{O, O, O, O}},
	}
	for mi, mode := range []string{"hidden", "anonymous", "orgs", "public"} {
		w.setVisibility(owner, mode, true)
		for _, who := range people {
			t.Run(mode+"/"+who.name, func(t *testing.T) {
				want := who.want[mi]
				// Страница.
				page, err := w.svc.Get(bg, id, who.p)
				switch want {
				case C:
					if !errors.Is(err, ErrNotFound) || page.Profile.ID != uuid.Nil {
						t.Fatalf("страница должна быть закрыта: %v %+v", err, page)
					}
				case M:
					if err != nil {
						t.Fatal(err)
					}
					p := page.Profile
					if !page.Viewer.Masked || p.Name != "" || p.Headline != "" || p.City != "" || p.Region != nil || p.About != "" ||
						p.ContactEmail != "" || p.Identifiers != (Identifiers{}) || p.Degree.Dissertation != "" || p.Degree.Institution != "" ||
						len(p.Sections.Publications)+len(p.Sections.Grants)+len(p.Sections.Experience) != 0 || len(p.Skills.Research) != 0 {
						t.Errorf("анонимный вид раскрыл лишнее: %+v", p)
					}
					if p.Degree.Level != DegreeCandidate || p.CareerLevel != 2 || len(p.Specialties) != 2 || p.HIndex.RSCI == nil || *p.HIndex.RSCI != 297 ||
						p.Counts.Publications != 1 || p.Counts.Grants != 1 || !p.OpenToOffers || p.Label != "Кандидат наук, 1.4.1, 1.4.4, R2, h-index 297" {
						t.Errorf("научный облик потерян: %+v", p)
					}
					noSecrets(t, "страница", page)
					if u := p.UpdatedAt; u != u.Truncate(24*time.Hour) {
						t.Errorf("у анонима время правки должно быть с точностью до дня: %v", u)
					}
				case O:
					if err != nil {
						t.Fatal(err)
					}
					if page.Viewer.Masked || page.Profile.Name != "Анонимов Секретный" || page.Profile.Headline == "" || len(page.Profile.Sections.Publications) != 1 {
						t.Errorf("полный вид неполон: %+v", page.Profile)
					}
				}

				// Резюме: только при полном раскрытии.
				if who.p != nil && who.p.ID != owner.ID {
					_, _, err := w.svc.CV(bg, *who.p, &id)
					if (want == O) != (err == nil) {
						t.Errorf("резюме: ошибка %v при уровне %v", err, want)
					}
					if want != O && !errors.Is(err, ErrNotFound) {
						t.Errorf("резюме закрытого и анонимного должно отвечать «нет такого»: %v", err)
					}
				}

				// Каталог (свой профиль владелец в нём не видит): тот же ответ, что и у страницы.
				if who.p != nil && who.p.ID == owner.ID {
					return
				}
				card, found := findCard(t, w, who.p, id)
				switch want {
				case C:
					if found {
						t.Errorf("закрытый профиль есть в каталоге: %+v", card)
					}
				case M:
					if !found || !card.Masked || card.Name != "" || card.Headline != "" || card.City != "" || card.Region != "" || card.Label == "" {
						t.Fatalf("анонимная карточка: %+v, найдена %v", card, found)
					}
					if card.Label != "Кандидат наук, 1.4.1, 1.4.4, R2, h-index 297" || card.CareerLevel != 2 {
						t.Errorf("подпись и уровень: %+v", card)
					}
					noSecrets(t, "каталог", card)
					if u := card.UpdatedAt; u != u.Truncate(24*time.Hour) {
						t.Errorf("у анонимной карточки время правки должно быть с точностью до дня: %v", u)
					}
				case O:
					if !found || card.Masked || card.Name != "Анонимов Секретный" || card.Headline == "" {
						t.Errorf("полная карточка: %+v, найдена %v", card, found)
					}
				}
			})
		}
	}
}

// findCard ищет профиль в каталоге по метке h-index (297) и номеру; перебирает страницы.
func findCard(t *testing.T, w *world, who *auth.User, id uuid.UUID) (CatalogCard, bool) {
	t.Helper()
	for offset := 0; ; offset += 50 {
		res := w.catalog(who, CatalogParams{HMin: 297, Limit: 50, Offset: offset})
		for _, c := range res.Items {
			if c.ID == id {
				return c, true
			}
		}
		if len(res.Items) < 50 {
			return CatalogCard{}, false
		}
	}
}

// Анонима нельзя найти по тому, что его выдаёт: слова из имени и места работы, регион, сортировка по имени (D-203).
func TestAnonymousProfilesAreNotFoundByIdentifyingSearch(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Анонимов Секретный")
	staff := w.staff("Сотрудник")
	in := secretCore()
	in.RegionCode = "54"
	if _, err := w.svc.SaveCore(bg, owner.User, in); err != nil {
		t.Fatal(err)
	}
	w.setVisibility(owner, "anonymous", false)
	own, _ := w.svc.Own(bg, owner.User)
	id := own.Profile.ID
	has := func(p CatalogParams) bool {
		p.Limit = 50
		for _, c := range w.catalog(&staff.User, p).Items {
			if c.ID == id {
				return true
			}
		}
		return false
	}
	// По научным условиям находится.
	for name, p := range map[string]CatalogParams{
		"степень": {HMin: 297, Degrees: []string{DegreeCandidate}}, "область науки": {HMin: 297, Fields: []string{"1.4"}},
		"звание": {HMin: 297, Titles: []string{TitleDocent}}, "открыт к предложениям": {HMin: 297, OpenOnly: false},
	} {
		if !has(p) {
			t.Errorf("анонимный профиль не найден по условию %q", name)
		}
	}
	// По тому, что выдаёт человека, нет.
	for name, p := range map[string]CatalogParams{
		"имя": {Query: "Анонимов"}, "должность": {Query: "Секретной лаборатории"}, "город": {Query: "Секретоград"},
		"о себе": {Query: "секретных катализаторов"}, "навык": {Query: "Секретная методика"},
		"регион": {HMin: 297, Region: "54"}, "имя похожими словами": {Query: "Анонимовв"},
	} {
		if has(p) {
			t.Errorf("анонимный профиль найден по условию %q", name)
		}
	}
	// После раскрытия имени профиль ведёт себя как «виден организациям».
	orgID, _ := orgOf(t, staff)
	reveal(t, id, orgID)
	for name, p := range map[string]CatalogParams{"имя": {Query: "Анонимов"}, "регион": {HMin: 297, Region: "54"}} {
		if !has(p) {
			t.Errorf("после раскрытия имя не находится по условию %q", name)
		}
	}
}

func TestHiddenOrgsLifecycle(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Учёный")
	a, b := w.staff("Сотрудник A"), w.staff("Сотрудник B")
	_, slugA := orgOf(t, a)
	_, slugB := orgOf(t, b)

	list, err := w.svc.HiddenOrgs(bg, owner.User)
	if err != nil || len(list) != 0 || list == nil {
		t.Fatalf("новый профиль: %v %v", list, err)
	}
	if list, err = w.svc.HideFrom(bg, owner.User, slugB); err != nil || len(list) != 1 || list[0].Slug != slugB || list[0].Name == "" || list[0].City == "" {
		t.Fatalf("скрыли B: %+v %v", list, err)
	}
	// Повтор ничего не меняет, список по алфавиту.
	if _, err = w.svc.HideFrom(bg, owner.User, slugB); err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if list, err = w.svc.HideFrom(bg, owner.User, slugA); err != nil || len(list) != 2 {
		t.Fatalf("скрыли A: %+v %v", list, err)
	}
	if list[0].Name > list[1].Name {
		t.Errorf("список не по алфавиту: %+v", list)
	}
	if n := countRows(t, `SELECT count(*) FROM profile_hidden_orgs h JOIN profiles p ON p.id = h.profile_id WHERE p.user_id = $1`, owner.ID); n != 2 {
		t.Errorf("в базе %d строк", n)
	}
	// Неизвестная организация.
	if _, err = w.svc.HideFrom(bg, owner.User, "no-such-org-xyz"); !errors.Is(err, ErrOrgNotFound) {
		t.Errorf("неизвестная организация: %v", err)
	}
	if _, err = w.svc.UnhideFrom(bg, owner.User, "no-such-org-xyz"); !errors.Is(err, ErrOrgNotFound) {
		t.Errorf("снять скрытие с неизвестной: %v", err)
	}
	// Снять скрытие; повторное снятие не ошибка.
	if list, err = w.svc.UnhideFrom(bg, owner.User, slugA); err != nil || len(list) != 1 || list[0].Slug != slugB {
		t.Fatalf("сняли A: %+v %v", list, err)
	}
	if list, err = w.svc.UnhideFrom(bg, owner.User, slugA); err != nil || len(list) != 1 {
		t.Fatalf("повторное снятие: %+v %v", list, err)
	}
	// Чужой список не виден и не меняется.
	other := w.user("Другой учёный")
	if list, _ = w.svc.HiddenOrgs(bg, other.User); len(list) != 0 {
		t.Errorf("чужой список: %+v", list)
	}
	// После снятия скрытия сотрудник A снова видит публичный профиль, сотрудник B нет.
	if _, err := w.svc.SaveCore(bg, owner.User, CoreInput{Headline: "Исследователь"}); err != nil {
		t.Fatal(err)
	}
	w.setVisibility(owner, "public", false)
	own, _ := w.svc.Own(bg, owner.User)
	if _, err := w.svc.Get(bg, own.Profile.ID, &a.User); err != nil {
		t.Errorf("сотрудник A после снятия: %v", err)
	}
	if _, err := w.svc.Get(bg, own.Profile.ID, &b.User); !errors.Is(err, ErrNotFound) {
		t.Errorf("сотрудник B: %v", err)
	}
}

func TestHiddenOrgsLimit(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Учёный с множеством скрытых")
	if _, err := w.svc.Own(bg, owner.User); err != nil {
		t.Fatal(err)
	}
	prefix := strings.ToLower(marker())
	if _, err := sharedPool.Exec(bg, `INSERT INTO organizations (slug, name, kind, city, created_at, updated_at)
		SELECT $1 || '-' || g, 'Организация ' || g, 'institute', 'Казань', now(), now() FROM generate_series(1, $2::int) g`, prefix, MaxHiddenOrgs+1); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= MaxHiddenOrgs; i++ {
		if _, err := w.svc.HideFrom(bg, owner.User, fmt.Sprintf("%s-%d", prefix, i)); err != nil {
			t.Fatalf("организация %d: %v", i, err)
		}
	}
	over := fmt.Sprintf("%s-%d", prefix, MaxHiddenOrgs+1)
	if _, err := w.svc.HideFrom(bg, owner.User, over); !errors.Is(err, ErrTooManyHidden) {
		t.Fatalf("пятьдесят первая: %v", err)
	}
	if n := countRows(t, `SELECT count(*) FROM profile_hidden_orgs h JOIN profiles p ON p.id = h.profile_id WHERE p.user_id = $1`, owner.ID); n != MaxHiddenOrgs {
		t.Errorf("в списке %d организаций", n)
	}
	// Уже скрытую организацию повторное скрытие не считает лишней.
	if _, err := w.svc.HideFrom(bg, owner.User, prefix+"-1"); err != nil {
		t.Errorf("повтор при полном списке: %v", err)
	}
	// Освободилось место: пятьдесят первая проходит.
	if _, err := w.svc.UnhideFrom(bg, owner.User, prefix+"-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.HideFrom(bg, owner.User, over); err != nil {
		t.Errorf("после освобождения места: %v", err)
	}
}

// Скрытие и раскрытие уходят вместе с аккаунтом.
func TestHiddenOrgsAndRevealsAreRemovedWithTheProfile(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Исчезающий")
	staff := w.staff("Сотрудник")
	orgID, slug := orgOf(t, staff)
	if _, err := w.svc.HideFrom(bg, owner.User, slug); err != nil {
		t.Fatal(err)
	}
	own, _ := w.svc.Own(bg, owner.User)
	reveal(t, own.Profile.ID, orgID)
	if _, err := sharedPool.Exec(bg, `DELETE FROM profiles WHERE id = $1`, own.Profile.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, `SELECT count(*) FROM profile_hidden_orgs WHERE profile_id = $1`, own.Profile.ID) + countRows(t, `SELECT count(*) FROM profile_reveals WHERE profile_id = $1`, own.Profile.ID); n != 0 {
		t.Errorf("остались строки: %d", n)
	}
}

// Маска строится из разрешённого списка полей: проверка на типе, чтобы новое поле View не попало в анонимный вид само.
func TestMaskKeepsOnlyAllowedFields(t *testing.T) {
	full := View{}
	// Заполняем каждое поле View, кроме разрешённых, непустым значением; затем маска обязана вернуть их нулевыми.
	full.ID = uuid.New()
	full.Name, full.Visibility, full.Headline, full.City, full.About, full.AcademicTitle = "n", "v", "h", "c", "a", "docent"
	full.Region = &Code{Code: "54", Name: "r"}
	full.Degree = DegreeInfo{Level: "candidate", Specialty: &Code{Code: "1.4.4"}, Year: ptr(2000), Institution: "i", Dissertation: "d"}
	full.AcademicTitleYear = ptr(2020)
	full.Identifiers = Identifiers{ORCID: "o", SPIN: "s", ScopusID: "sc", WosID: "w"}
	full.Skills = Skills{Research: []string{"x"}, General: []string{"y"}}
	full.ContactEmail = "e@example.ru"
	full.Sections.Publications = []Item{{ID: uuid.New()}}
	full.Counts = Counts{Publications: 1}
	got := mask(full)
	if got.Name != "" || got.Visibility != "" || got.Headline != "" || got.City != "" || got.About != "" || got.Region != nil ||
		got.Degree.Year != nil || got.Degree.Institution != "" || got.Degree.Dissertation != "" || got.AcademicTitleYear != nil ||
		got.Identifiers != (Identifiers{}) || len(got.Skills.Research)+len(got.Skills.General) != 0 || got.ContactEmail != "" ||
		len(got.Sections.Publications) != 0 {
		t.Errorf("маска пропустила лишнее: %+v", got)
	}
	if got.ID != full.ID || got.Counts.Publications != 1 || got.Degree.Specialty == nil || got.AcademicTitle != "docent" {
		t.Errorf("маска потеряла разрешённое: %+v", got)
	}
	raw, _ := json.Marshal(got)
	for _, null := range []string{`"skills":null`, `"research":null`, `"publications":null`} {
		if strings.Contains(string(raw), null) {
			t.Errorf("пустые списки должны быть [], не null: %s", raw)
		}
	}
	if bestHIndex(HIndex{}) != 0 || bestHIndex(HIndex{RSCI: ptr(3), WoS: ptr(8)}) != 8 {
		t.Errorf("bestHIndex")
	}
}
