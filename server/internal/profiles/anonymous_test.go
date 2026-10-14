package profiles

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
)

// Срез 15: анонимный режим (D-199) и «скрыть от моей организации» (D-200).

// anonOwner — человек с полностью заполненным профилем в анонимном режиме; заодно приметы, которых в безымянной
// карточке быть не должно ни в каком виде.
func anonOwner(t *testing.T, w *world, name string) (person, []string) {
	t.Helper()
	p := w.user(name)
	if _, err := w.svc.SaveCore(bg, p.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	pub := w.addItem(p, goodPublication())
	w.addItem(p, ItemInput{Kind: KindEducation, ItemFields: ItemFields{Institution: "Новосибирский государственный университет", Program: "Химия", YearFrom: ptr(2008), YearTo: ptr(2013)}})
	w.addItem(p, ItemInput{Kind: KindExperience, ItemFields: ItemFields{Organization: "Институт катализа СО РАН", Position: "Младший научный сотрудник", YearFrom: ptr(2014)}})
	w.addItem(p, ItemInput{Kind: KindGrant, ItemFields: ItemFields{Title: "Грант на изучение оксидов", Funder: "РНФ", Number: "22-73-10001", Role: "lead", YearFrom: ptr(2022)}})
	w.setVisibility(p, "anon", true)
	secrets := []string{
		name, "Старший научный сотрудник", "лаборатория катализа", `"city":"Новосибирск"`, "Новосибирск,", "Изучаю активные центры", "Институт катализа",
		"0000-0002-1825-0097", "1234-5678", "57190123456", "a-1234-2008", "Elena-Orlova", "github.com", "orlova@example.ru", "Orlova@Example.ru",
		"ИК-спектроскопия", "Английский B2", "Активные центры оксидных катализаторов", "Новосибирский государственный университет",
		"Грант на изучение оксидов", "22-73-10001", pub.Title, "10.1234/abc.2023", "Журнал физической химии",
		"Младший научный сотрудник", "2016", "2021",
	}
	return p, secrets
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// noLeak проверяет, что ни одной приметы нет в тексте (без учёта регистра). Год «2016» и «2021» ищутся как отдельные
// слова JSON (после двоеточия), чтобы не путаться с номерами.
func noLeak(t *testing.T, what, text string, secrets []string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, s := range secrets {
		needle := strings.ToLower(s)
		switch s {
		case "2016", "2021":
			needle = ":" + s
		}
		if strings.Contains(lower, needle) {
			t.Errorf("%s: в ответе есть «%s»", what, s)
		}
	}
}

func TestAnonymousProfileForStaffIsBlind(t *testing.T) {
	w := newWorld(t)
	owner, secrets := anonOwner(t, w, "Анна Седова")
	staff := w.staff("Сотрудник организации")
	page := ownPage(t, w, owner)

	got, err := w.svc.Get(bg, page.Profile.ID, &staff.User)
	if err != nil {
		t.Fatalf("сотруднику организации анонимный профиль виден: %v", err)
	}
	p := got.Profile
	if !p.Anonymous || p.Name != "" || p.Headline != "" || p.City != "" || p.About != "" || p.ContactEmail != "" || p.Visibility != "" {
		t.Errorf("в безымянной карточке остались личные поля: %+v", p)
	}
	if p.Identifiers != (Identifiers{}) || p.Degree.Year != nil || p.Degree.Institution != "" || p.Degree.Dissertation != "" || p.AcademicTitleYear != nil {
		t.Errorf("в безымянной карточке остались номера или подробности степени: %+v", p)
	}
	if len(p.Skills.Research)+len(p.Skills.General) != 0 {
		t.Errorf("навыки в безымянной карточке: %+v", p.Skills)
	}
	s := p.Sections
	if len(s.Education)+len(s.Experience)+len(s.Publications)+len(s.Grants)+len(s.Patents)+len(s.Teaching) != 0 {
		t.Errorf("записи разделов в безымянной карточке: %+v", s)
	}
	// Остаётся научное: степень, звание, специальности, регион, метрики, счётчики.
	if p.Degree.Level != DegreeCandidate || p.Degree.Specialty == nil || p.Degree.Specialty.Code != "1.4.4" || p.AcademicTitle != TitleDocent {
		t.Errorf("пропали степень или звание: %+v", p)
	}
	if len(p.Specialties) != 2 || p.Region == nil || p.Region.Code != "54" || !p.OpenToOffers {
		t.Errorf("пропали специальности, регион или отметка: %+v", p)
	}
	if p.HIndex.RSCI == nil || *p.HIndex.RSCI != 12 || p.HIndex.Scholar == nil || *p.HIndex.Scholar != 15 {
		t.Errorf("пропал h-index: %+v", p.HIndex)
	}
	if p.PublicationCount == nil || *p.PublicationCount != 1 {
		t.Errorf("число публикаций = %v, ожидали 1", p.PublicationCount)
	}
	if !p.UpdatedAt.IsZero() {
		t.Errorf("в безымянной карточке время правки профиля %v: по нему сопоставляют с известными событиями", p.UpdatedAt)
	}
	if got.Viewer.CanSeeContacts || got.Viewer.IsOwner {
		t.Errorf("viewer = %+v", got.Viewer)
	}
	noLeak(t, "страница", jsonOf(t, got), secrets)

	// Резюме безымянной карточки: тот же отбор.
	pdf, name, err := w.svc.CV(bg, staff.User, &page.Profile.ID)
	if err != nil {
		t.Fatalf("резюме: %v", err)
	}
	if name != "CV.pdf" {
		t.Errorf("имя файла резюме = %q, в нём не должно быть имени человека", name)
	}
	if len(pdf) == 0 {
		t.Error("пустой файл резюме")
	}
	doc := cvDocument(got, "SciBox", time.Now())
	noLeak(t, "резюме", jsonOf(t, doc), secrets)
	if doc.Title != "Анонимный соискатель" || doc.Subtitle != "Кандидат наук, 1.4.4, Доцент" {
		t.Errorf("заголовок резюме: %q / %q", doc.Title, doc.Subtitle)
	}
}

// Владелец видит свой анонимный профиль целиком, остальные без входа и без организации не видят его вовсе.
func TestAnonymousProfileWhoSees(t *testing.T) {
	w := newWorld(t)
	owner, _ := anonOwner(t, w, "Борис Светлов")
	stranger := w.user("Вошедший без организации")
	id := ownPage(t, w, owner).Profile.ID

	full, err := w.svc.Get(bg, id, &owner.User)
	if err != nil || full.Profile.Anonymous || full.Profile.Name != "Борис Светлов" || full.Profile.Visibility != "anon" || full.Profile.Headline == "" {
		t.Errorf("владелец: %+v, %v", full.Profile, err)
	}
	for name, viewer := range map[string]*auth.User{"аноним": nil, "вошедший без организации": &stranger.User} {
		if _, err := w.svc.Get(bg, id, viewer); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, ожидали ErrNotFound", name, err)
		}
	}
}

// Каждая пара «режим × смотрящий» возвращает анонимную карточку только там, где так задумано.
func TestVisibilityModesTable(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Владимир Ершов")
	if _, err := w.svc.SaveCore(bg, owner.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	id := ownPage(t, w, owner).Profile.ID
	staff := w.staff("Сотрудник")
	stranger := w.user("Без организации")
	tests := []struct {
		mode      string
		viewer    string
		wantFound bool
		wantBlind bool
	}{
		{"hidden", "staff", false, false},
		{"hidden", "stranger", false, false},
		{"hidden", "guest", false, false},
		{"anon", "staff", true, true},
		{"anon", "stranger", false, false},
		{"anon", "guest", false, false},
		{"orgs", "staff", true, false},
		{"orgs", "stranger", false, false},
		{"orgs", "guest", false, false},
		{"public", "staff", true, false},
		{"public", "stranger", true, false},
		{"public", "guest", true, false},
	}
	viewers := map[string]*auth.User{"staff": &staff.User, "stranger": &stranger.User, "guest": nil}
	for _, tc := range tests {
		t.Run(tc.mode+"/"+tc.viewer, func(t *testing.T) {
			w.setVisibility(owner, tc.mode, false)
			got, err := w.svc.Get(bg, id, viewers[tc.viewer])
			if tc.wantFound != (err == nil) {
				t.Fatalf("err = %v, найден должен быть = %v", err, tc.wantFound)
			}
			if err == nil && got.Profile.Anonymous != tc.wantBlind {
				t.Errorf("Anonymous = %v, ожидали %v", got.Profile.Anonymous, tc.wantBlind)
			}
			if err == nil && tc.wantBlind && (got.Profile.Name != "" || got.Profile.ContactEmail != "") {
				t.Errorf("утечка в безымянной карточке: %+v", got.Profile)
			}
		})
	}
}

func ownPage(t *testing.T, w *world, p person) Page {
	t.Helper()
	page, err := w.svc.Own(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// orgOf — первая организация человека (у владельца своей организации она единственная).
func orgOf(t *testing.T, p person) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := sharedPool.QueryRow(bg, `SELECT org_id FROM org_members WHERE user_id = $1`, p.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestHiddenFromOrganization(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Галина Воронцова")
	if _, err := w.svc.SaveCore(bg, owner.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	staffA, staffB := w.staff("Сотрудник А"), w.staff("Сотрудник Б")
	orgA, orgB := orgOf(t, staffA), orgOf(t, staffB)
	id := ownPage(t, w, owner).Profile.ID

	// Сотрудник двух организаций: одна из них скрыта, другая нет.
	both := w.staff("Сотрудник двух организаций")
	other, err := w.orgs.CreateOrganization(bg, both.User, orgInput())
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	orgBoth := orgOf(t, both) // первая из организаций

	page, err := w.svc.SetHiddenOrgs(bg, owner.User, []uuid.UUID{orgA, orgBoth, orgA})
	if err != nil {
		t.Fatalf("SetHiddenOrgs: %v", err)
	}
	if len(page.HiddenOrgs) != 2 {
		t.Fatalf("hidden_orgs = %+v, ожидали 2 без повтора", page.HiddenOrgs)
	}
	for _, mode := range []string{"anon", "orgs", "public"} {
		w.setVisibility(owner, mode, false)
		if _, err := w.svc.Get(bg, id, &staffA.User); !errors.Is(err, ErrNotFound) {
			t.Errorf("режим %s: сотрудник скрытой организации видит профиль (err = %v)", mode, err)
		}
		if _, err := w.svc.Get(bg, id, &both.User); !errors.Is(err, ErrNotFound) {
			t.Errorf("режим %s: сотрудник двух организаций, одна из них скрыта, видит профиль (err = %v)", mode, err)
		}
		if _, err := w.svc.Get(bg, id, &staffB.User); err != nil {
			t.Errorf("режим %s: сотрудник другой организации не видит профиль: %v", mode, err)
		}
		if _, _, err := w.svc.CV(bg, staffA.User, &id); !errors.Is(err, ErrNotFound) {
			t.Errorf("режим %s: резюме открылось скрытой организации (err = %v)", mode, err)
		}
		// Каталог: в списке скрытой организации человека нет, у другой есть.
		hasCard := func(v person) bool {
			res, err := w.svc.Catalog(bg, CatalogParams{}, &v.User)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range res.Items {
				if c.ID == id {
					return true
				}
			}
			return false
		}
		if hasCard(staffA) || hasCard(both) {
			t.Errorf("режим %s: карточка есть в каталоге скрытой организации", mode)
		}
		if !hasCard(staffB) {
			t.Errorf("режим %s: карточки нет в каталоге другой организации", mode)
		}
	}
	// Владелец смотрит свой профиль, как раньше.
	if got, err := w.svc.Get(bg, id, &owner.User); err != nil || got.Profile.Name == "" {
		t.Errorf("владелец: %v", err)
	}

	// Список можно изменить и очистить: профиль открывается снова.
	if _, err := w.svc.SetHiddenOrgs(bg, owner.User, []uuid.UUID{orgB}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, id, &staffA.User); err != nil {
		t.Errorf("после замены списка профиль закрыт: %v", err)
	}
	if _, err := w.svc.Get(bg, id, &staffB.User); !errors.Is(err, ErrNotFound) {
		t.Errorf("новая скрытая организация видит профиль: %v", err)
	}
	cleared, err := w.svc.SetHiddenOrgs(bg, owner.User, nil)
	if err != nil || len(cleared.HiddenOrgs) != 0 {
		t.Fatalf("очистка: %+v, %v", cleared.HiddenOrgs, err)
	}
	if _, err := w.svc.Get(bg, id, &staffB.User); err != nil {
		t.Errorf("после очистки профиль закрыт: %v", err)
	}
}

func TestSetHiddenOrgsValidation(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Дмитрий Орлов")
	var verr *auth.ValidationError
	if _, err := w.svc.SetHiddenOrgs(bg, owner.User, []uuid.UUID{uuid.New()}); !errors.As(err, &verr) || verr.Fields["org_ids"] == "" {
		t.Errorf("несуществующая организация: %v", err)
	}
	many := make([]uuid.UUID, MaxHiddenOrgs+1)
	for i := range many {
		many[i] = uuid.New()
	}
	if _, err := w.svc.SetHiddenOrgs(bg, owner.User, many); !errors.As(err, &verr) || verr.Fields["org_ids"] == "" {
		t.Errorf("слишком много организаций: %v", err)
	}
	// Нулевой номер отбрасывается, как повтор.
	if page, err := w.svc.SetHiddenOrgs(bg, owner.User, []uuid.UUID{uuid.Nil}); err != nil || len(page.HiddenOrgs) != 0 {
		t.Errorf("нулевой номер: %+v, %v", page.HiddenOrgs, err)
	}
	// Организация удалена вместе со списком: каскад не оставляет мусора.
	staff := w.staff("Сотрудник")
	org := orgOf(t, staff)
	if _, err := w.svc.SetHiddenOrgs(bg, owner.User, []uuid.UUID{org}); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedPool.Exec(bg, `DELETE FROM organizations WHERE id = $1`, org); err != nil {
		t.Fatal(err)
	}
	if page := ownPage(t, w, owner); len(page.HiddenOrgs) != 0 {
		t.Errorf("hidden_orgs после удаления организации = %+v", page.HiddenOrgs)
	}
}

// Каталог: анонимная карточка без имени, поиск по имени и должности её не находит, порядок по алфавиту ставит её в конец.
func TestCatalogAnonymousCard(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Ксения Ярмоленко")
	in := goodCore()
	in.HRsci = ptr(299) // редкий h-index отделяет профили этого теста от остальных в общей базе
	if _, err := w.svc.SaveCore(bg, owner.User, in); err != nil {
		t.Fatal(err)
	}
	pub := w.addItem(owner, goodPublication())
	w.setVisibility(owner, "anon", true)
	id := ownPage(t, w, owner).Profile.ID

	named := w.user("Яков Абрамов")
	in2 := goodCore()
	in2.HRsci = ptr(299)
	if _, err := w.svc.SaveCore(bg, named.User, in2); err != nil {
		t.Fatal(err)
	}
	w.setVisibility(named, "orgs", false)
	namedID := ownPage(t, w, named).Profile.ID

	staff, stranger := w.staff("Сотрудник"), w.user("Без организации")
	ids := func(res CatalogResult) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(res.Items))
		for _, c := range res.Items {
			out = append(out, c.ID)
		}
		return out
	}

	res := w.catalog(&staff.User, CatalogParams{HMin: 299})
	if len(res.Items) != 2 {
		t.Fatalf("в каталоге %d карточек, ожидали 2", len(res.Items))
	}
	var card CatalogCard
	for _, c := range res.Items {
		if c.ID == id {
			card = c
		}
	}
	if !card.Anonymous || card.Name != "" || card.Headline != "" || card.City != "" || !card.UpdatedAt.IsZero() {
		t.Errorf("анонимная карточка: %+v", card)
	}
	if card.Degree != DegreeCandidate || card.AcademicTitle != TitleDocent || card.Region != "Новосибирская область" || len(card.Specialties) != 2 ||
		card.HIndex == nil || *card.HIndex != 299 || card.Publications != 1 || !card.OpenToOffers {
		t.Errorf("в анонимной карточке пропали научные данные: %+v", card)
	}
	noLeak(t, "карточка каталога", jsonOf(t, card), []string{"Ксения", "Ярмоленко", "лаборатория катализа", `"city":"Новосибирск"`, "Новосибирск,", pub.Title, "0000-0002-1825-0097", "Orlova@Example.ru"})

	// Не видят: аноним и вошедший без организации (режим «организациям» и анонимный им закрыты).
	for name, who := range map[string]*auth.User{"аноним": nil, "вошедший без организации": &stranger.User} {
		if got := ids(w.catalog(who, CatalogParams{HMin: 299})); len(got) != 0 {
			t.Errorf("%s видит карточки: %v", name, got)
		}
	}

	// Поиск по имени, должности, городу и «о себе» анонимную карточку не находит, даже «по похожим словам».
	for _, q := range []string{"Ярмоленко", "Ксения", "Ярмоленков", "лаборатория катализа", "активные центры катализаторов", "ИК-спектроскопия"} {
		r := w.catalog(&staff.User, CatalogParams{Query: q, HMin: 299})
		if slices.Contains(ids(r), id) {
			t.Errorf("анонимная карточка найдена по запросу «%s» (fuzzy = %v)", q, r.Fuzzy)
		}
	}
	// А по научным данным находится: специальность, регион.
	for _, p := range []CatalogParams{{Query: "физическая химия", HMin: 299}, {Query: "Новосибирская", HMin: 299}, {Fields: []string{"1.4"}, HMin: 299}, {Degrees: []string{"candidate"}, HMin: 299}, {Region: "54", HMin: 299, Q12Min: 0}} {
		if r := w.catalog(&staff.User, p); !slices.Contains(ids(r), id) {
			t.Errorf("анонимная карточка не найдена по %+v", p)
		}
	}

	// По алфавиту анонимная карточка идёт после именных: положение в списке не выдаёт первую букву имени.
	if got := ids(w.catalog(&staff.User, CatalogParams{HMin: 299, Sort: SortName})); !slices.Equal(got, []uuid.UUID{namedID, id}) {
		t.Errorf("порядок по алфавиту = %v, ожидали именной, потом анонимный", got)
	}

	// Режим «организациям»: имя находится поиском, возврат к анонимному прячет его снова (триггер на смену режима).
	w.setVisibility(owner, "orgs", true)
	if r := w.catalog(&staff.User, CatalogParams{Query: "Ярмоленко", HMin: 299}); !slices.Contains(ids(r), id) {
		t.Error("в режиме «организациям» человек не находится по фамилии")
	}
	w.setVisibility(owner, "anon", true)
	if r := w.catalog(&staff.User, CatalogParams{Query: "Ярмоленко", HMin: 299}); slices.Contains(ids(r), id) {
		t.Error("после возврата в анонимный режим человек всё ещё находится по фамилии")
	}
}

// Собственная анонимная карточка в каталоге владельца не показывается, как и любая собственная.
func TestCatalogOwnAnonymousProfileIsNotListed(t *testing.T) {
	w := newWorld(t)
	owner := w.staff("Сотрудник-учёный")
	in := goodCore()
	in.HRsci = ptr(298)
	if _, err := w.svc.SaveCore(bg, owner.User, in); err != nil {
		t.Fatal(err)
	}
	w.setVisibility(owner, "anon", false)
	own := ownPage(t, w, owner).Profile.ID
	for _, c := range w.catalog(&owner.User, CatalogParams{HMin: 298}).Items {
		if c.ID == own {
			t.Errorf("своя карточка в каталоге: %+v", c)
		}
	}
}

// Режим anon принимается и сохраняется, неизвестный отвергается; смена режима не трогает скрытие от организаций.
func TestSetPrivacyAnonymous(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Громова")
	page, err := w.svc.SetPrivacy(bg, p.User, "anon", true)
	if err != nil || page.Profile.Visibility != "anon" || !page.Profile.OpenToOffers {
		t.Fatalf("SetPrivacy anon: %+v, %v", page.Profile, err)
	}
	// Владелец видит заранее, как его увидят организации: безымянная карточка с научными данными и уровнем.
	if _, err := w.svc.SaveCore(bg, p.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	own := ownPage(t, w, p)
	prev := own.AnonymousPreview
	if prev == nil || !prev.Anonymous || prev.Name != "" || prev.Headline != "" || prev.Level != 2 || prev.Degree.Level != DegreeCandidate {
		t.Errorf("предпросмотр анонимной карточки: %+v", prev)
	}
	if own.Profile.Name != "Елена Громова" || own.Profile.Anonymous {
		t.Errorf("собственная страница владельца изменилась: %+v", own.Profile)
	}
	var verr *auth.ValidationError
	if _, err := w.svc.SetPrivacy(bg, p.User, "anonymous", true); !errors.As(err, &verr) || verr.Fields["visibility"] == "" {
		t.Errorf("неизвестный режим: %v", err)
	}
	org := orgOf(t, w.staff("Сотрудник"))
	if _, err := w.svc.SetHiddenOrgs(bg, p.User, []uuid.UUID{org}); err != nil {
		t.Fatal(err)
	}
	page, err = w.svc.SetPrivacy(bg, p.User, "public", false)
	if err != nil || len(page.HiddenOrgs) != 1 {
		t.Errorf("смена режима затронула список скрытых организаций: %+v, %v", page.HiddenOrgs, err)
	}
}

// Отправляемое с откликом: человек откликается сам, организация видит профиль целиком, даже если он анонимный (D-079).
func TestApplicationSnapshotIsFullForAnonymousProfile(t *testing.T) {
	w := newWorld(t)
	owner, _ := anonOwner(t, w, "Лидия Мартынова")
	pkg, err := w.svc.ForApplication(bg, owner.User, "lidia@example.ru")
	if err != nil {
		t.Fatal(err)
	}
	v := pkg.Profile
	if v.Anonymous || v.Name != "Лидия Мартынова" || v.Headline == "" || v.Identifiers.ORCID == "" || len(v.Sections.Publications) != 1 || v.Visibility != "" {
		t.Errorf("снимок для отклика: %+v", v)
	}
	if pkg.CVName == "CV.pdf" {
		t.Errorf("в имени резюме отклика должно быть имя человека, получили %q", pkg.CVName)
	}
}
