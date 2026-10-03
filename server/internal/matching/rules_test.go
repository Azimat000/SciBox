package matching

import (
	"reflect"
	"testing"
)

func lvl(n int) *int { return &n }

func TestProfileLevel(t *testing.T) {
	for _, c := range []struct {
		name          string
		degree, title string
		want          int
	}{
		{"без степени", "none", "none", 1},
		{"пустые поля", "", "", 1},
		{"аспирант со званием доцента", "none", "docent", 1},
		{"кандидат наук", "candidate", "none", 2},
		{"кандидат наук, доцент", "candidate", "docent", 2},
		{"кандидат наук со званием профессора", "candidate", "professor", 2},
		{"доктор наук", "doctor", "none", 3},
		{"доктор наук, доцент", "doctor", "docent", 3},
		{"доктор наук, профессор", "doctor", "professor", 4},
	} {
		if got := (Profile{Degree: c.degree, Title: c.title}).Level(); got != c.want {
			t.Errorf("%s: R%d, ожидали R%d", c.name, got, c.want)
		}
	}
}

func TestProfileReadyAndGroups(t *testing.T) {
	if (Profile{}).Ready() {
		t.Error("профиль без специальностей не готов к подбору")
	}
	p := Profile{Specialties: []string{"1.4.4", "1.4.2", "2.3.1", "10.1.1", "1.1.1"}}
	if !p.Ready() {
		t.Error("профиль со специальностями готов")
	}
	// Без повторов и по порядку строк.
	if got, want := p.Groups(), []string{"1.1", "1.4", "10.1", "2.3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("группы %v, ожидали %v", got, want)
	}
	if got := (Profile{}).Groups(); len(got) != 0 {
		t.Errorf("пустой профиль: %v", got)
	}
	// Код без номера группы (только раздел или пустой) группой не считается.
	if got := (Profile{Specialties: []string{"", "1"}}).Groups(); len(got) != 0 {
		t.Errorf("неполные коды: %v", got)
	}
}

func TestCommonSegments(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.4.4", "1.4.4", 3}, {"1.4.4", "1.4.2", 2}, {"1.4.4", "1.2.2", 1}, {"1.4.4", "2.4.4", 0},
		{"1.4", "1.4.4", 2}, {"1", "1.4.4", 1}, {"10.1.1", "1.1.1", 0}, {"", "1", 0},
	} {
		if got := commonSegments(c.a, c.b); got != c.want {
			t.Errorf("%s и %s: %d, ожидали %d", c.a, c.b, got, c.want)
		}
	}
}

func TestScore(t *testing.T) {
	base := Profile{Specialties: []string{"1.4.4"}, Degree: "candidate", Title: "none", Region: "54"}
	for _, c := range []struct {
		name    string
		profile Profile
		open    Opening
		ok      bool
		score   int
		reasons []string
	}{
		{
			name: "точная специальность, тот же уровень, степень, регион", profile: base, ok: true, score: 40 + 25 + 15 + 20,
			open:    Opening{Specialties: []string{"1.4.4"}, CareerLevel: lvl(2), Degree: "candidate", Region: "54", Format: "onsite"},
			reasons: []string{ReasonSpecialty, ReasonLevel, ReasonDegree, ReasonRegion},
		},
		{
			name: "та же группа", profile: base, ok: true, score: 28,
			open: Opening{Specialties: []string{"1.4.2"}, Degree: "none"}, reasons: []string{ReasonGroup},
		},
		{
			name: "тот же раздел, но другая группа — не подходит", profile: base, ok: false,
			open: Opening{Specialties: []string{"1.1.1"}, CareerLevel: lvl(2), Degree: "none", Region: "54"},
		},
		{
			name: "лучшая из нескольких специальностей вакансии", profile: base, ok: true, score: 40,
			open: Opening{Specialties: []string{"1.1.1", "1.4.4", "1.2.2"}, Degree: "none"}, reasons: []string{ReasonSpecialty},
		},
		{
			name: "нет общей области — не подходит", profile: base, ok: false,
			open: Opening{Specialties: []string{"2.3.1"}, Degree: "none"},
		},
		{
			name: "у вакансии нет специальностей — не подходит", profile: base, ok: false,
			open: Opening{Degree: "none"},
		},
		{
			name: "у человека нет специальностей — не подходит", profile: Profile{Degree: "doctor"}, ok: false,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none"},
		},
		{
			name: "соседний уровень выше", profile: base, ok: true, score: 40 + 10,
			open: Opening{Specialties: []string{"1.4.4"}, CareerLevel: lvl(3), Degree: "none"}, reasons: []string{ReasonSpecialty, ReasonLevelNear},
		},
		{
			name: "соседний уровень ниже", profile: base, ok: true, score: 40 + 10,
			open: Opening{Specialties: []string{"1.4.4"}, CareerLevel: lvl(1), Degree: "none"}, reasons: []string{ReasonSpecialty, ReasonLevelNear},
		},
		{
			name: "далёкий уровень баллов не даёт", profile: base, ok: true, score: 40,
			open: Opening{Specialties: []string{"1.4.4"}, CareerLevel: lvl(4), Degree: "none"}, reasons: []string{ReasonSpecialty},
		},
		{
			name: "требуется степень выше — не подходит", profile: base, ok: false,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "doctor"},
		},
		{
			name: "без степени на вакансию со степенью — не подходит", profile: Profile{Specialties: []string{"1.4.4"}, Degree: "none"}, ok: false,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "candidate"},
		},
		{
			name: "степень выше требуемой даёт меньше", profile: Profile{Specialties: []string{"1.4.4"}, Degree: "doctor"}, ok: true, score: 40 + 8,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "candidate"}, reasons: []string{ReasonSpecialty, ReasonDegree},
		},
		{
			name: "требуется звание выше — не подходит", profile: Profile{Specialties: []string{"1.4.4"}, Degree: "doctor", Title: "docent"}, ok: false,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none", Title: "professor"},
		},
		{
			name: "звание отвечает требованию", profile: Profile{Specialties: []string{"1.4.4"}, Degree: "doctor", Title: "professor"}, ok: true, score: 40 + 10,
			open: Opening{Specialties: []string{"1.4.4"}, CareerLevel: lvl(3), Degree: "none", Title: "docent"}, reasons: []string{ReasonSpecialty, ReasonLevelNear},
		},
		{
			name: "удалённая работа без совпавшего региона", profile: base, ok: true, score: 40 + 12,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none", Region: "77", Format: "remote"}, reasons: []string{ReasonSpecialty, ReasonRemote},
		},
		{
			name: "удалённая работа в своём регионе считается регионом", profile: base, ok: true, score: 40 + 20,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none", Region: "54", Format: "remote"}, reasons: []string{ReasonSpecialty, ReasonRegion},
		},
		{
			name: "чужой регион, очно", profile: base, ok: true, score: 40,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none", Region: "77", Format: "onsite"}, reasons: []string{ReasonSpecialty},
		},
		{
			name: "у человека не указан регион", profile: Profile{Specialties: []string{"1.4.4"}, Degree: "none"}, ok: true, score: 40,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none", Region: ""}, reasons: []string{ReasonSpecialty},
		},
		{
			name: "гибрид региона не заменяет", profile: base, ok: true, score: 40,
			open: Opening{Specialties: []string{"1.4.4"}, Degree: "none", Region: "77", Format: "hybrid"}, reasons: []string{ReasonSpecialty},
		},
	} {
		got, ok := Score(c.profile, c.open)
		if ok != c.ok {
			t.Errorf("%s: подходит = %v, ожидали %v", c.name, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Score != c.score || !reflect.DeepEqual(got.Reasons, c.reasons) {
			t.Errorf("%s: %d %v, ожидали %d %v", c.name, got.Score, got.Reasons, c.score, c.reasons)
		}
	}
}

// Наибольшая сумма баллов равна 100: так задумано, и шкала не должна расти незаметно.
func TestScoreMaximumIsHundred(t *testing.T) {
	p := Profile{Specialties: []string{"1.4.4"}, Degree: "candidate", Region: "54"}
	m, ok := Score(p, Opening{Specialties: []string{"1.4.4"}, CareerLevel: lvl(2), Degree: "candidate", Region: "54"})
	if !ok || m.Score != 100 {
		t.Errorf("лучший случай: %d (ок=%v)", m.Score, ok)
	}
}
