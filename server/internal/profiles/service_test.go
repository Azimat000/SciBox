package profiles

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/crossref"
)

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var verr *auth.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("ожидали ошибку полей, получили %v", err)
	}
	return verr.Fields
}

func TestOwnCreatesEmptyHiddenProfile(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	page, err := w.svc.Own(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	v := page.Profile
	if v.Name != "Елена Орлова" || v.Visibility != "hidden" || v.OpenToOffers || v.Degree.Level != DegreeNone || v.AcademicTitle != TitleNone {
		t.Errorf("пустой профиль: %+v", v)
	}
	if v.Region != nil || v.Degree.Specialty != nil || v.Degree.Year != nil || v.HIndex.RSCI != nil {
		t.Errorf("пустые связи: %+v", v)
	}
	if v.Specialties == nil || v.Sections.Education == nil || v.Sections.Publications == nil || v.Sections.Teaching == nil {
		t.Errorf("пустые списки должны быть списками, не null: %+v", v)
	}
	if !page.Viewer.IsOwner || !page.Viewer.CanSeeContacts {
		t.Errorf("владелец: %+v", page.Viewer)
	}
	// Повторное обращение возвращает тот же профиль и не создаёт второй.
	again, err := w.svc.Own(bg, p.User)
	if err != nil || again.Profile.ID != v.ID {
		t.Errorf("повторно: %v %v", again.Profile.ID, err)
	}
	if n := countRows(t, `SELECT count(*) FROM profiles WHERE user_id = $1`, p.ID); n != 1 {
		t.Errorf("профилей %d", n)
	}
}

func TestSaveCoreRoundTrip(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	page, err := w.svc.SaveCore(bg, p.User, goodCore())
	if err != nil {
		t.Fatal(err)
	}
	v := page.Profile
	if v.Headline != "Старший научный сотрудник, лаборатория катализа" || v.City != "Новосибирск" || v.Region == nil || v.Region.Code != "54" || v.Region.Name == "" {
		t.Errorf("основное: %+v", v)
	}
	if v.Degree.Level != DegreeCandidate || v.Degree.Specialty == nil || v.Degree.Specialty.Code != "1.4.4" || v.Degree.Specialty.Name == "" || *v.Degree.Year != 2016 {
		t.Errorf("степень: %+v", v.Degree)
	}
	if v.AcademicTitle != TitleDocent || *v.AcademicTitleYear != 2021 {
		t.Errorf("звание: %v %v", v.AcademicTitle, v.AcademicTitleYear)
	}
	if v.Identifiers != (Identifiers{ORCID: "0000-0002-1825-0097", SPIN: "12345678", ScopusID: "57190123456", WosID: "A-1234-2008"}) {
		t.Errorf("идентификаторы: %+v", v.Identifiers)
	}
	if *v.HIndex.RSCI != 12 || *v.HIndex.Scopus != 9 || *v.HIndex.WoS != 7 || *v.HIndex.Scholar != 15 {
		t.Errorf("h-index: %+v", v.HIndex)
	}
	if v.ContactEmail != "orlova@example.ru" || v.About != "Изучаю активные центры катализаторов.\nЛюблю эксперименты." {
		t.Errorf("контакты и текст: %q %q", v.ContactEmail, v.About)
	}
	if len(v.Specialties) != 2 || v.Specialties[0].Code != "1.4.1" || v.Specialties[1].Code != "1.4.4" {
		t.Errorf("специальности (по номерам): %+v", v.Specialties)
	}
	if v.Visibility != "hidden" {
		t.Errorf("сохранение основных полей не должно менять приватность: %q", v.Visibility)
	}

	// Вторая правка заменяет всё, в том числе специальности; степень «нет» стирает подробности.
	next := CoreInput{Headline: "Профессор", Degree: DegreeNone, Specialties: []string{"1.4.4"}}
	page, err = w.svc.SaveCore(bg, p.User, next)
	if err != nil {
		t.Fatal(err)
	}
	v = page.Profile
	if v.Headline != "Профессор" || v.Region != nil || v.Degree.Specialty != nil || v.Degree.Year != nil || v.ContactEmail != "" || v.Identifiers.ORCID != "" || v.HIndex.RSCI != nil {
		t.Errorf("после замены: %+v", v)
	}
	if len(v.Specialties) != 1 {
		t.Errorf("специальности после замены: %+v", v.Specialties)
	}
	// Совсем без специальностей.
	page, err = w.svc.SaveCore(bg, p.User, CoreInput{})
	if err != nil || len(page.Profile.Specialties) != 0 {
		t.Errorf("без специальностей: %v %+v", err, page.Profile.Specialties)
	}
}

func TestSaveCoreChecksReferences(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	tests := []struct {
		name  string
		edit  func(*CoreInput)
		field string
	}{
		{"регион неизвестный", func(c *CoreInput) { c.RegionCode = "99" }, "region_code"},
		{"специальность степени неизвестная", func(c *CoreInput) { c.DegreeSpecialty = "9.9.9" }, "degree_specialty_code"},
		{"специальность неизвестная", func(c *CoreInput) { c.Specialties = []string{"1.4.4", "9.9.9"} }, "specialties"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := goodCore()
			tc.edit(&in)
			_, err := w.svc.SaveCore(bg, p.User, in)
			if got := fieldsOf(t, err); got[tc.field] == "" {
				t.Errorf("ошибки: %v", got)
			}
		})
	}
	// Неудачное сохранение ничего не меняет.
	page, _ := w.svc.Own(bg, p.User)
	if page.Profile.Headline != "" || len(page.Profile.Specialties) != 0 {
		t.Errorf("профиль изменился после отказа: %+v", page.Profile)
	}
}

func TestSaveCoreKeepsItemsAndPrivacy(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	w.setVisibility(p, "public", true)
	w.addItem(p, goodPublication())
	page, err := w.svc.SaveCore(bg, p.User, goodCore())
	if err != nil {
		t.Fatal(err)
	}
	if page.Profile.Visibility != "public" || !page.Profile.OpenToOffers || len(page.Profile.Sections.Publications) != 1 {
		t.Errorf("основное сохранение задело чужое: %+v", page.Profile)
	}
}

func TestSetPrivacy(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	for _, vis := range []string{"orgs", "public", "hidden"} {
		page, err := w.svc.SetPrivacy(bg, p.User, vis, vis == "orgs")
		if err != nil {
			t.Fatal(err)
		}
		if page.Profile.Visibility != vis || page.Profile.OpenToOffers != (vis == "orgs") {
			t.Errorf("%s: %+v", vis, page.Profile)
		}
	}
	for _, bad := range []string{"", "Public", "friends"} {
		if _, err := w.svc.SetPrivacy(bg, p.User, bad, true); fieldsOf(t, err)["visibility"] == "" {
			t.Errorf("режим %q принят", bad)
		}
	}
	page, _ := w.svc.Own(bg, p.User)
	if page.Profile.Visibility != "hidden" {
		t.Errorf("отказ изменил режим: %q", page.Profile.Visibility)
	}
}

// Каждая пара «кто смотрит × режим»: видит ли профиль, видит ли контакты, видит ли служебные поля владельца.
func TestWhoSeesWhat(t *testing.T) {
	w := newWorld(t)
	v := w.viewers()
	if _, err := w.svc.SaveCore(bg, v.owner.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	pub := w.addItem(v.owner, goodPublication())
	own, _ := w.svc.Own(bg, v.owner.User)
	id := own.Profile.ID

	type who struct {
		name   string
		p      *auth.User
		member bool // сотрудник организации
		owner  bool
	}
	people := []who{
		{"аноним", nil, false, false},
		{"вошедший без организации", &v.stranger.User, false, false},
		{"сотрудник организации", &v.staff.User, true, false},
		{"владелец", &v.owner.User, false, true},
	}
	// режим → кто видит профиль
	sees := map[string]map[string]bool{
		"hidden": {"аноним": false, "вошедший без организации": false, "сотрудник организации": false, "владелец": true},
		"orgs":   {"аноним": false, "вошедший без организации": false, "сотрудник организации": true, "владелец": true},
		"public": {"аноним": true, "вошедший без организации": true, "сотрудник организации": true, "владелец": true},
	}
	for _, mode := range []string{"hidden", "orgs", "public"} {
		w.setVisibility(v.owner, mode, true)
		for _, who := range people {
			t.Run(mode+"/"+who.name, func(t *testing.T) {
				page, err := w.svc.Get(bg, id, who.p)
				if !sees[mode][who.name] {
					if !errors.Is(err, ErrNotFound) {
						t.Fatalf("профиль не должен открываться: %v", err)
					}
					if page.Profile.Name != "" || page.Profile.ID != uuid.Nil {
						t.Errorf("вместе с отказом ушли данные: %+v", page)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				p := page.Profile
				if p.Name != "Владелец профиля" || p.Headline == "" || len(p.Sections.Publications) != 1 || p.Sections.Publications[0].ID != pub.ID {
					t.Errorf("содержимое: %+v", p)
				}
				if !p.OpenToOffers {
					t.Errorf("«открыт к предложениям» видят все, кто видит профиль")
				}
				wantContacts := who.owner || who.member
				if (p.ContactEmail != "") != wantContacts || page.Viewer.CanSeeContacts != wantContacts {
					t.Errorf("контакты: %q, CanSeeContacts=%v, ожидали %v", p.ContactEmail, page.Viewer.CanSeeContacts, wantContacts)
				}
				if (p.Visibility != "") != who.owner || page.Viewer.IsOwner != who.owner {
					t.Errorf("служебное: visibility=%q, IsOwner=%v", p.Visibility, page.Viewer.IsOwner)
				}
			})
		}
	}
}

func TestGetUnknownLooksLikeHidden(t *testing.T) {
	w := newWorld(t)
	p := w.user("Скрытая")
	own, _ := w.svc.Own(bg, p.User)
	_, hidden := w.svc.Get(bg, own.Profile.ID, nil)
	_, unknown := w.svc.Get(bg, uuid.New(), nil)
	if !errors.Is(hidden, ErrNotFound) || !errors.Is(unknown, ErrNotFound) || hidden.Error() != unknown.Error() {
		t.Errorf("скрытый и несуществующий должны отвечать одинаково: %v / %v", hidden, unknown)
	}
}

func TestStaffBecomesVisibleAfterJoining(t *testing.T) {
	// Человек без организации не видит профиль «организациям», а после создания организации видит.
	w := newWorld(t)
	owner, viewer := w.user("Владелец"), w.user("Зритель")
	own, _ := w.svc.Own(bg, owner.User)
	w.setVisibility(owner, "orgs", false)
	if _, err := w.svc.Get(bg, own.Profile.ID, &viewer.User); !errors.Is(err, ErrNotFound) {
		t.Fatalf("без организации: %v", err)
	}
	if _, err := w.orgs.CreateOrganization(bg, viewer.User, orgInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, own.Profile.ID, &viewer.User); err != nil {
		t.Errorf("с организацией: %v", err)
	}
}

func TestItemsLifecycle(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	// По одной записи каждого вида.
	added := map[string]Item{}
	for _, kind := range Kinds {
		added[kind] = w.addItem(p, ItemInput{Kind: kind, ItemFields: validItem(kind)})
		if added[kind].ID == uuid.Nil || added[kind].Kind != kind {
			t.Fatalf("%s: %+v", kind, added[kind])
		}
	}
	page, _ := w.svc.Own(bg, p.User)
	s := page.Profile.Sections
	if len(s.Education) != 1 || len(s.Experience) != 1 || len(s.Publications) != 1 || len(s.Grants) != 1 || len(s.Patents) != 1 || len(s.Teaching) != 1 {
		t.Fatalf("разделы: %+v", s)
	}
	if s.Publications[0].DOI != "10.1234/abc.2023" || s.Grants[0].Funder != "РНФ" || s.Patents[0].Number != "RU 2 745 123" || s.Teaching[0].Course != "Физическая химия" || s.Experience[0].Position == "" || s.Education[0].Institution != "НГУ" {
		t.Errorf("поля записей потерялись: %+v", s)
	}

	// Правка.
	edit := validItem(KindPublication)
	edit.Title, edit.Year = "Другое название", ptr(2024)
	up, err := w.svc.UpdateItem(bg, p.User, added[KindPublication].ID, ItemInput{ItemFields: edit})
	if err != nil || up.Title != "Другое название" || up.Kind != KindPublication || up.ID != added[KindPublication].ID {
		t.Fatalf("правка: %+v %v", up, err)
	}
	// Вид можно повторить в теле, но не изменить.
	if _, err := w.svc.UpdateItem(bg, p.User, added[KindPublication].ID, ItemInput{Kind: KindPublication, ItemFields: edit}); err != nil {
		t.Errorf("тот же вид: %v", err)
	}
	if _, err := w.svc.UpdateItem(bg, p.User, added[KindPublication].ID, ItemInput{Kind: KindGrant, ItemFields: edit}); fieldsOf(t, err)["kind"] == "" {
		t.Errorf("смена вида принята")
	}
	// Правка с ошибкой ничего не меняет.
	bad := edit
	bad.Title = ""
	if _, err := w.svc.UpdateItem(bg, p.User, added[KindPublication].ID, ItemInput{ItemFields: bad}); fieldsOf(t, err)["title"] == "" {
		t.Errorf("пустое название принято")
	}
	page, _ = w.svc.Own(bg, p.User)
	if page.Profile.Sections.Publications[0].Title != "Другое название" {
		t.Errorf("отказ изменил запись: %+v", page.Profile.Sections.Publications[0])
	}

	// Удаление.
	if err := w.svc.DeleteItem(bg, p.User, added[KindGrant].ID); err != nil {
		t.Fatal(err)
	}
	page, _ = w.svc.Own(bg, p.User)
	if len(page.Profile.Sections.Grants) != 0 || len(page.Profile.Sections.Publications) != 1 {
		t.Errorf("после удаления: %+v", page.Profile.Sections)
	}
	if err := w.svc.DeleteItem(bg, p.User, added[KindGrant].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	if _, err := w.svc.UpdateItem(bg, p.User, added[KindGrant].ID, ItemInput{ItemFields: validItem(KindGrant)}); !errors.Is(err, ErrNotFound) {
		t.Errorf("правка удалённой: %v", err)
	}
}

func TestItemsOrderNewestFirst(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	mk := func(org string, from int, to *int) {
		w.addItem(p, ItemInput{Kind: KindExperience, ItemFields: ItemFields{Organization: org, Position: "Научный сотрудник", YearFrom: ptr(from), YearTo: to}})
	}
	mk("Старая", 2005, ptr(2010))
	mk("Нынешняя", 2018, nil)
	mk("Средняя", 2010, ptr(2018))
	for _, y := range []int{2019, 2024, 2021} {
		in := goodPublication()
		in.DOI, in.Year = "", ptr(y)
		w.addItem(p, in)
	}
	page, _ := w.svc.Own(bg, p.User)
	var orgs []string
	for _, it := range page.Profile.Sections.Experience {
		orgs = append(orgs, it.Organization)
	}
	if strings.Join(orgs, ",") != "Нынешняя,Средняя,Старая" {
		t.Errorf("опыт: %v", orgs)
	}
	var yrs []string
	for _, it := range page.Profile.Sections.Publications {
		yrs = append(yrs, fmt.Sprint(*it.Year))
	}
	if strings.Join(yrs, ",") != "2024,2021,2019" {
		t.Errorf("публикации: %v", yrs)
	}
}

func TestItemsAreOwnedByTheirAuthor(t *testing.T) {
	w := newWorld(t)
	owner, other := w.user("Хозяин"), w.user("Чужой")
	it := w.addItem(owner, goodPublication())

	if err := w.svc.DeleteItem(bg, other.User, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужое удаление: %v", err)
	}
	edit := validItem(KindPublication)
	edit.Title = "Захвачено"
	if _, err := w.svc.UpdateItem(bg, other.User, it.ID, ItemInput{ItemFields: edit}); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужая правка: %v", err)
	}
	page, _ := w.svc.Own(bg, owner.User)
	if len(page.Profile.Sections.Publications) != 1 || page.Profile.Sections.Publications[0].Title == "Захвачено" {
		t.Errorf("чужой человек изменил запись: %+v", page.Profile.Sections.Publications)
	}
	if n := countRows(t, `SELECT count(*) FROM profile_items i JOIN profiles p ON p.id = i.profile_id WHERE p.user_id = $1`, other.ID); n != 0 {
		t.Errorf("у чужого появились записи: %d", n)
	}
	if err := w.svc.DeleteItem(bg, other.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующая запись: %v", err)
	}
}

func TestAddItemUnknownKind(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена")
	for _, kind := range []string{"", "hobby", "publications"} {
		if _, err := w.svc.AddItem(bg, p.User, ItemInput{Kind: kind}); fieldsOf(t, err)["kind"] == "" {
			t.Errorf("вид %q принят", kind)
		}
	}
}

func TestItemLimitPerSection(t *testing.T) {
	old := maxItemsKind[KindGrant]
	maxItemsKind[KindGrant] = 2
	t.Cleanup(func() { maxItemsKind[KindGrant] = old })
	w := newWorld(t)
	p := w.user("Елена")
	in := ItemInput{Kind: KindGrant, ItemFields: validItem(KindGrant)}
	w.addItem(p, in)
	second := w.addItem(p, in)
	if _, err := w.svc.AddItem(bg, p.User, in); !errors.Is(err, ErrTooMany) {
		t.Fatalf("третья запись: %v", err)
	}
	// Другие разделы считаются отдельно, а после удаления место освобождается.
	w.addItem(p, ItemInput{Kind: KindTeaching, ItemFields: validItem(KindTeaching)})
	if err := w.svc.DeleteItem(bg, p.User, second.ID); err != nil {
		t.Fatal(err)
	}
	w.addItem(p, in)
}

func TestDuplicateDOI(t *testing.T) {
	w := newWorld(t)
	p, other := w.user("Елена"), w.user("Другая")
	first := w.addItem(p, goodPublication())

	// Тот же DOI в другой записи, в другом регистре и с адресом: нельзя.
	dup := goodPublication()
	dup.DOI = "https://doi.org/10.1234/ABC.2023"
	if _, err := w.svc.AddItem(bg, p.User, dup); fieldsOf(t, err)["doi"] != msgDOIDuplicate {
		t.Errorf("дубль принят: %v", err)
	}
	// Правка той же записи с её DOI допустима.
	edit := goodPublication()
	edit.Title = "Новое название"
	if _, err := w.svc.UpdateItem(bg, p.User, first.ID, edit); err != nil {
		t.Errorf("правка своей записи: %v", err)
	}
	// Правка другой записи на занятый DOI нельзя.
	second := goodPublication()
	second.DOI = "10.1234/other"
	other2 := w.addItem(p, second)
	if _, err := w.svc.UpdateItem(bg, p.User, other2.ID, goodPublication()); fieldsOf(t, err)["doi"] != msgDOIDuplicate {
		t.Errorf("правка на занятый DOI: %v", err)
	}
	// У другого человека тот же DOI можно.
	if _, err := w.svc.AddItem(bg, other.User, goodPublication()); err != nil {
		t.Errorf("у другого человека: %v", err)
	}
	// Публикации без DOI не конфликтуют между собой.
	for i := 0; i < 2; i++ {
		in := goodPublication()
		in.DOI = ""
		w.addItem(p, in)
	}
}

func TestSameDOIAddedConcurrentlyOnlyOnce(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена")
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = w.svc.AddItem(bg, p.User, goodPublication())
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		} else if fieldsOf(t, err)["doi"] != msgDOIDuplicate {
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("записей создано %d, ожидали 1", ok)
	}
}

func TestLookupDOI(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена")

	work, err := w.svc.LookupDOI(bg, p.User, " https://doi.org/10.1234/ABC.2023 ")
	if err != nil {
		t.Fatal(err)
	}
	if work.DOI != "10.1234/abc.2023" || work.Title != "Найденная статья" || w.doi.calls[0] != "10.1234/abc.2023" {
		t.Errorf("%+v %v", work, w.doi.calls)
	}

	// Неверный DOI до Crossref не доходит.
	before := w.doi.count()
	if _, err := w.svc.LookupDOI(bg, p.User, "nature"); fieldsOf(t, err)["doi"] != msgDOIInvalid || w.doi.count() != before {
		t.Errorf("неверный DOI: %v", err)
	}

	// Уже добавленный DOI: Crossref не спрашиваем.
	w.addItem(p, goodPublication())
	before = w.doi.count()
	if _, err := w.svc.LookupDOI(bg, p.User, "10.1234/abc.2023"); fieldsOf(t, err)["doi"] != msgDOIDuplicate || w.doi.count() != before {
		t.Errorf("дубль: %v", err)
	}

	// Ответы Crossref.
	w.doi.err = crossref.ErrNotFound
	if _, err := w.svc.LookupDOI(bg, p.User, "10.1234/none"); !errors.Is(err, ErrDOINotFound) {
		t.Errorf("не найден: %v", err)
	}
	w.doi.err = fmt.Errorf("%w: сбой", crossref.ErrUnavailable)
	if _, err := w.svc.LookupDOI(bg, p.User, "10.1234/none"); !errors.Is(err, ErrDOIUnavailable) {
		t.Errorf("недоступен: %v", err)
	}
	w.doi.err = errors.New("что-то странное")
	if _, err := w.svc.LookupDOI(bg, p.User, "10.1234/none"); !errors.Is(err, ErrDOIUnavailable) {
		t.Errorf("прочее: %v", err)
	}
}

func TestLookupDOIRateLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.DOI = Limit{Max: 3, Window: time.Hour}
	p, other := w.user("Елена"), w.user("Другая")
	for i := 0; i < 3; i++ {
		if _, err := w.svc.LookupDOI(bg, p.User, fmt.Sprintf("10.1234/x%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var rerr *auth.RateLimitedError
	_, err := w.svc.LookupDOI(bg, p.User, "10.1234/x9")
	if !errors.As(err, &rerr) || rerr.RetryAfter <= 0 || rerr.RetryAfter > time.Hour {
		t.Fatalf("четвёртый поиск: %v", err)
	}
	if got := w.doi.count(); got != 3 {
		t.Errorf("Crossref спросили %d раз", got)
	}
	// Лимит у каждого свой и со временем проходит.
	if _, err := w.svc.LookupDOI(bg, other.User, "10.1234/x1"); err != nil {
		t.Errorf("другой человек: %v", err)
	}
	w.clock.Advance(time.Hour + time.Minute)
	if _, err := w.svc.LookupDOI(bg, p.User, "10.1234/x9"); err != nil {
		t.Errorf("после часа: %v", err)
	}
}

func TestCVRespectsPrivacy(t *testing.T) {
	w := newWorld(t)
	v := w.viewers()
	if _, err := w.svc.SaveCore(bg, v.owner.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	w.addItem(v.owner, goodPublication())
	own, _ := w.svc.Own(bg, v.owner.User)
	id := own.Profile.ID

	pdf, name, err := w.svc.CV(bg, v.owner.User, nil)
	if err != nil || len(pdf) < 1000 || string(pdf[:5]) != "%PDF-" || name != "Владелец профиля — CV.pdf" {
		t.Fatalf("собственное резюме: %d байт, %q, %v", len(pdf), name, err)
	}
	// Скрытый профиль: чужие резюме не получают; свой владелец получает и по номеру.
	if _, _, err := w.svc.CV(bg, v.staff.User, &id); !errors.Is(err, ErrNotFound) {
		t.Errorf("скрытый, сотрудник: %v", err)
	}
	if _, _, err := w.svc.CV(bg, v.owner.User, &id); err != nil {
		t.Errorf("скрытый, владелец: %v", err)
	}
	w.setVisibility(v.owner, "orgs", false)
	if _, _, err := w.svc.CV(bg, v.stranger.User, &id); !errors.Is(err, ErrNotFound) {
		t.Errorf("организациям, вошедший без организации: %v", err)
	}
	if _, _, err := w.svc.CV(bg, v.staff.User, &id); err != nil {
		t.Errorf("организациям, сотрудник: %v", err)
	}
	w.setVisibility(v.owner, "public", false)
	if _, _, err := w.svc.CV(bg, v.stranger.User, &id); err != nil {
		t.Errorf("публичный, вошедший: %v", err)
	}
	if _, _, err := w.svc.CV(bg, v.owner.User, &[]uuid.UUID{uuid.New()}[0]); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий: %v", err)
	}
}

func TestCVFileName(t *testing.T) {
	tests := map[string]string{
		"Елена Орлова":       "Елена Орлова — CV.pdf",
		"  Иван  ":           "Иван — CV.pdf",
		`А/б\в:г*д?е"ж<з>и|`: "Абвгдежзи — CV.pdf",
		"":                   "CV.pdf",
		"///":                "CV.pdf",
	}
	for in, want := range tests {
		if got := cvFileName(in); got != want {
			t.Errorf("cvFileName(%q) = %q, ожидали %q", in, got, want)
		}
	}
}

func TestProfileIsRemovedWithAccount(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена")
	w.addItem(p, goodPublication())
	if _, err := w.svc.SaveCore(bg, p.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedPool.Exec(bg, `DELETE FROM users WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, `SELECT count(*) FROM profiles WHERE user_id = $1`, p.ID); n != 0 {
		t.Errorf("профиль остался: %d", n)
	}
	if n := countRows(t, `SELECT count(*) FROM profile_items WHERE data ->> 'doi' = '10.1234/abc.2023' AND profile_id NOT IN (SELECT id FROM profiles)`); n != 0 {
		t.Errorf("записи остались без профиля: %d", n)
	}
}

func TestDefaultConfigAndNewService(t *testing.T) {
	cfg := DefaultConfig("SciBox")
	if cfg.ProductName != "SciBox" || cfg.DOI.Max != 60 || cfg.DOI.Window != time.Hour {
		t.Errorf("%+v", cfg)
	}
	s := NewService(sharedPool, &fakeDOI{}, cfg)
	if s == nil || s.now == nil || s.q == nil {
		t.Fatal("сервис не собран")
	}
	w := newWorld(t)
	p := w.user("Елена")
	if _, err := s.Own(bg, p.User); err != nil {
		t.Errorf("сервис на настоящем пуле: %v", err)
	}
}
