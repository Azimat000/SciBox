package orgs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/auth"
)

func TestCreateOrganization(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван Петров")
	in := orgInput()
	in.Name = "  Институт   Тестовых Наук " + strings.Repeat("и", 3)
	org, err := w.svc.CreateOrganization(bg, owner.User, in)
	if err != nil {
		t.Fatal(err)
	}
	if org.Slug == "" || org.Name == "" || org.Website != "https://example.ru" || org.Kind != KindInstitute {
		t.Fatalf("unexpected organization: %+v", org)
	}
	if strings.Contains(org.Name, "  ") {
		t.Errorf("name keeps double spaces: %q", org.Name)
	}
	mine, err := w.svc.MyOrganizations(bg, owner.User)
	if err != nil || len(mine) != 1 || mine[0].Role != "owner" || mine[0].Slug != org.Slug {
		t.Fatalf("my organizations = %+v, %v", mine, err)
	}
}

func TestCreateOrganizationRejectsBadInput(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	before := countRows(t, "SELECT count(*) FROM organizations")
	_, err := w.svc.CreateOrganization(bg, owner.User, OrgInput{Name: "", Kind: "bank", City: ""})
	if f := fieldsOf(t, err); f["name"] == "" || f["kind"] == "" || f["city"] == "" {
		t.Fatalf("fields = %v", f)
	}
	if after := countRows(t, "SELECT count(*) FROM organizations"); after != before {
		t.Error("nothing must be created for invalid input")
	}
}

// Одинаковые названия получают разные адреса; 21-й повтор берёт случайный хвост.
func TestCreateOrganizationSlugCollisions(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.Create = Limit{Max: 1000, Window: time.Hour}
	owner := w.user("Иван")
	in := orgInput()
	in.Name = "Лаборатория слизней и ящериц"
	seen := map[string]bool{}
	for i := 0; i < maxSlugTries+3; i++ {
		org, err := w.svc.CreateOrganization(bg, owner.User, in)
		if err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
		if seen[org.Slug] {
			t.Fatalf("duplicate slug %q", org.Slug)
		}
		seen[org.Slug] = true
	}
	base := slugify(in.Name)
	if !seen[base] || !seen[base+"-2"] {
		t.Errorf("expected %q and %q among %v", base, base+"-2", seen)
	}
	randomTail := 0
	for s := range seen {
		if len(s) == len(base)+1+8 && strings.HasPrefix(s, base+"-") && !strings.HasPrefix(s, base+"-2") {
			randomTail++
		}
	}
	if randomTail == 0 {
		t.Errorf("after %d tries a random suffix must be used: %v", maxSlugTries, seen)
	}
}

func TestCreateOrganizationReservedSlug(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	in := orgInput()
	in.Name = "New"
	org, err := w.svc.CreateOrganization(bg, owner.User, in)
	if err != nil {
		t.Fatal(err)
	}
	if org.Slug == "new" {
		t.Errorf("slug %q is reserved for the 'create' page", org.Slug)
	}
}

func TestCreateOrganizationIsRateLimited(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	for i := 0; i < w.svc.cfg.Create.Max; i++ {
		w.org(owner)
	}
	_, err := w.svc.CreateOrganization(bg, owner.User, orgInput())
	var rl *auth.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter < time.Hour {
		t.Fatalf("err = %v, want a rate limit with a long wait", err)
	}
	if got := countRows(t, "SELECT count(*) FROM organizations WHERE created_by = $1", owner.ID); got != w.svc.cfg.Create.Max {
		t.Errorf("created %d organizations, want %d", got, w.svc.cfg.Create.Max)
	}
	// Другому человеку это не мешает.
	other := w.user("Анна")
	w.org(other)
	// Через сутки лимит снова свободен.
	w.clock.Advance(25 * time.Hour)
	w.org(owner)
}

func TestUpdateOrganization(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	in := orgInput()
	in.Name = "Новое название"
	in.Kind = KindUniversity
	in.Website = ""
	got, err := w.svc.UpdateOrganization(bg, owner.User, org.Slug, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Новое название" || got.Kind != KindUniversity || got.Website != "" || got.Slug != org.Slug {
		t.Fatalf("got %+v: slug must stay %q", got, org.Slug)
	}
	if _, err := w.svc.UpdateOrganization(bg, owner.User, org.Slug, OrgInput{}); fieldsOf(t, err)["name"] == "" {
		t.Error("invalid input must be rejected")
	}
	if _, err := w.svc.UpdateOrganization(bg, owner.User, "no-such-org", in); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	stranger := w.user("Чужой")
	if _, err := w.svc.UpdateOrganization(bg, stranger.User, org.Slug, in); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestGetOrganization(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван Петров")
	org := w.org(owner)
	lab := w.unit(owner, org.Slug)
	head := w.user("Анна Смирнова")
	w.member(owner, org.Slug, head, "unit_head", &lab.ID)
	if _, err := w.svc.CreateUnit(bg, owner.User, org.Slug, UnitInput{Name: "Кафедра без темы", Kind: UnitDepartment}); err != nil {
		t.Fatal(err)
	}

	// Аноним видит страницу без прав и без номера руководителя.
	page, err := w.svc.GetOrganization(bg, org.Slug, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Viewer != nil || len(page.Units) != 2 {
		t.Fatalf("anonymous page: %+v", page)
	}
	for _, u := range page.Units {
		if u.HeadUserID != nil {
			t.Error("anonymous visitor must not see the head's account id")
		}
		if u.Topics == nil {
			t.Error("topics must be an empty list, not null")
		}
	}
	var seenHead bool
	for _, u := range page.Units {
		if u.ID == lab.ID {
			seenHead = u.HeadName != nil && *u.HeadName == "Анна Смирнова"
		}
	}
	if !seenHead {
		t.Error("the unit's head name must be public")
	}

	// Вошедший чужой: viewer есть, прав нет.
	stranger := w.user("Чужой")
	page, _ = w.svc.GetOrganization(bg, org.Slug, &stranger.User)
	if page.Viewer == nil || page.Viewer.Role != "" || page.Viewer.CanEditOrganization || page.Viewer.CanManageMembers || page.Viewer.CanManageUnits || len(page.Viewer.EditableUnits) != 0 {
		t.Fatalf("stranger viewer: %+v", page.Viewer)
	}
	if page.Units[0].HeadUserID != nil || page.Units[1].HeadUserID != nil {
		t.Error("a stranger must not see account ids")
	}

	// Владелец: всё можно, все подразделения редактируемые, номера руководителей видны.
	page, _ = w.svc.GetOrganization(bg, org.Slug, &owner.User)
	if v := page.Viewer; v.Role != "owner" || !v.CanEditOrganization || !v.CanManageMembers || !v.CanManageUnits || len(v.EditableUnits) != 2 {
		t.Fatalf("owner viewer: %+v", v)
	}
	var withHeadID bool
	for _, u := range page.Units {
		if u.HeadUserID != nil && *u.HeadUserID == head.ID {
			withHeadID = true
		}
	}
	if !withHeadID {
		t.Error("members must see the head's account id")
	}

	// Руководитель: только своё подразделение.
	page, _ = w.svc.GetOrganization(bg, org.Slug, &head.User)
	if v := page.Viewer; v.Role != "unit_head" || v.CanEditOrganization || v.CanManageMembers || v.CanManageUnits || len(v.EditableUnits) != 1 || v.EditableUnits[0] != lab.ID {
		t.Fatalf("head viewer: %+v", v)
	}

	if _, err := w.svc.GetOrganization(bg, "no-such-org", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListOrganizations(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.Create = Limit{Max: 1000, Window: time.Hour}
	owner := w.user("Иван")
	// Метка делает названия и города этого теста уникальными среди остальных тестов общей базы.
	tag := fmt.Sprintf("Зюзя-%d-", orgSeq.Add(1))
	mk := func(name, kind, city, desc string) {
		t.Helper()
		in := OrgInput{Name: tag + " " + name, Kind: kind, City: tag + "-" + city, Description: desc}
		if _, err := w.svc.CreateOrganization(bg, owner.User, in); err != nil {
			t.Fatal(err)
		}
	}
	mk("Альфа институт", KindInstitute, "Томск", "")
	mk("Бета университет", KindUniversity, "Казань", strings.Repeat("слово ", 100))
	mk("Гамма 100% центр", KindScienceCenter, "Томск", "Коротко")
	mk("Дельта_лаб", KindRDCompany, "Пермь", "")

	names := func(f ListFilter) []string {
		t.Helper()
		res, err := w.svc.ListOrganizations(bg, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, i := range res.Items {
			out = append(out, strings.TrimPrefix(i.Name, tag+" "))
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	eq(names(ListFilter{Query: tag}), "Альфа институт", "Бета университет", "Гамма 100% центр", "Дельта_лаб")
	eq(names(ListFilter{Query: strings.ToLower(tag) + "-ТОМСК"}), "Альфа институт", "Гамма 100% центр") // город, без учёта регистра
	eq(names(ListFilter{Query: strings.ToUpper(tag) + " бета"}), "Бета университет")                    // название, кириллица
	eq(names(ListFilter{Query: tag, Kind: KindUniversity}), "Бета университет")
	eq(names(ListFilter{Query: tag, Kind: "no-such-kind"}))
	eq(names(ListFilter{Query: tag + " Гамма 100%"}), "Гамма 100% центр")
	eq(names(ListFilter{Query: tag + " Гамма 1%"})) // % ищется как знак, а не как «любые знаки»
	eq(names(ListFilter{Query: tag + " Дельта_лаб"}), "Дельта_лаб")
	eq(names(ListFilter{Query: tag + " Дельт_"})) // _ тоже знак: в названии «Дельта_», а не «Дельт_»
	eq(names(ListFilter{Query: tag + ` \`}))
	eq(names(ListFilter{Query: tag, Limit: 2}), "Альфа институт", "Бета университет")
	eq(names(ListFilter{Query: tag, Limit: 2, Offset: 2}), "Гамма 100% центр", "Дельта_лаб")
	eq(names(ListFilter{Query: tag, Offset: -5}), "Альфа институт", "Бета университет", "Гамма 100% центр", "Дельта_лаб")

	res, err := w.svc.ListOrganizations(bg, ListFilter{Query: tag, Limit: 1000})
	if err != nil || res.Total != 4 || len(res.Items) != 4 {
		t.Fatalf("total = %d, items = %d, err = %v", res.Total, len(res.Items), err)
	}
	for _, it := range res.Items {
		if strings.HasPrefix(it.Name, tag+" Бета") && !strings.HasSuffix(it.Summary, "…") {
			t.Errorf("a long description must be cut with an ellipsis: %q", it.Summary)
		}
		if strings.HasPrefix(it.Name, tag+" Гамма") && it.Summary != "Коротко" {
			t.Errorf("a short description stays whole: %q", it.Summary)
		}
	}
	res, _ = w.svc.ListOrganizations(bg, ListFilter{Query: tag, Limit: 2})
	if res.Total != 4 || len(res.Items) != 2 {
		t.Errorf("total must count all matches: total = %d, items = %d", res.Total, len(res.Items))
	}
	// Очень длинный запрос обрезается, а не ломает поиск.
	if res, err := w.svc.ListOrganizations(bg, ListFilter{Query: strings.Repeat("я", 500)}); err != nil || res.Total != 0 {
		t.Errorf("long query: %+v, %v", res, err)
	}
}

func TestListOrganizationsCountsUnits(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	w.unit(owner, org.Slug)
	w.unit(owner, org.Slug)
	res, err := w.svc.ListOrganizations(bg, ListFilter{Query: org.Name})
	if err != nil || len(res.Items) != 1 || res.Items[0].UnitCount != 2 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestSummaryAndLikePattern(t *testing.T) {
	if got := likePattern("  "); got != "" {
		t.Errorf("blank query: %q", got)
	}
	if got := likePattern(`50%_\x`); got != `%50\%\_\\x%` {
		t.Errorf("escape: %q", got)
	}
	if got := summaryOf(strings.Repeat("я", summaryRunes)); strings.HasSuffix(got, "…") {
		t.Error("a description of exactly the limit stays whole")
	}
	if got := summaryOf("Раз   два\nтри"); got != "Раз два три" {
		t.Errorf("whitespace: %q", got)
	}
}

func TestDefaultConfigAndClock(t *testing.T) {
	cfg := DefaultConfig("SciBox", "https://scibox.example/")
	if cfg.PublicURL != "https://scibox.example" || cfg.InviteTTL != 7*24*time.Hour {
		t.Errorf("config: %+v", cfg)
	}
	s := NewService(sharedPool, nil, cfg, nil)
	if d := time.Since(s.now()); d < -time.Second || d > time.Second {
		t.Errorf("the default clock is off by %v", d)
	}
}
