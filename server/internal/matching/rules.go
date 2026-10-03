// Package matching — то, что помогает ищущему не потерять вакансию: избранное, сохранённые поиски с рассылкой,
// «Подходящие вам», календарь сроков и напоминания (срез 11).
//
// Критичная зона (docs/TESTING.md, «подбор и рассылки»): письмо не должно уйти дважды, не туда, не про чужое и не про то,
// что человек не просил. Правила подбора простые и объяснимые (D-014): никакого ИИ, у каждой подсказки есть причина.
package matching

import (
	"sort"
	"strings"
)

// Степень и звание: порядок «выше — больше».
var (
	degreeRank = map[string]int{"none": 0, "candidate": 1, "doctor": 2}
	titleRank  = map[string]int{"none": 0, "docent": 1, "professor": 2}
)

// Причины, по которым вакансия подходит. Сайт подписывает их словами; сервер отдаёт только коды.
const (
	ReasonSpecialty = "specialty"  // совпала специальность ВАК
	ReasonGroup     = "group"      // та же группа специальностей (1.4)
	ReasonLevel     = "level"      // уровень R совпал
	ReasonLevelNear = "level_near" // уровень R соседний
	ReasonDegree    = "degree"     // степень отвечает требованию вакансии
	ReasonRegion    = "region"     // регион вакансии совпал с регионом профиля
	ReasonRemote    = "remote"     // работа удалённая
)

// Баллы правил. Сумма не больше 100: 40 + 25 + 15 + 20. Общий раздел науки (первое число кода) не даёт ничего: он слишком
// широк (в разделе 1 и физика, и биология, и науки о Земле), подбор требует хотя бы общую группу специальностей (1.4).
const (
	pointsSpecialty = 40
	pointsGroup     = 28
	pointsLevel     = 25
	pointsLevelNear = 10
	pointsDegree    = 15
	pointsDegreeAbv = 8
	pointsRegion    = 20
	pointsRemote    = 12
)

// Profile — то, что подбор знает о человеке: области науки, степень, звание, регион.
type Profile struct {
	Specialties []string // коды ВАК вида 1.4.4
	Degree      string   // none | candidate | doctor
	Title       string   // none | docent | professor
	Region      string   // код региона; пусто, если не указан
}

// Opening — то, что подбор знает о вакансии.
type Opening struct {
	Specialties []string
	CareerLevel *int   // 1–4 или nil
	Degree      string // требуемая степень
	Title       string // требуемое звание
	Region      string
	Format      string // onsite | hybrid | remote
}

// Match — результат правил для одной вакансии.
type Match struct {
	Score   int
	Reasons []string
}

// Ready — достаточно ли в профиле, чтобы подбирать: без областей науки подбирать не по чему.
func (p Profile) Ready() bool { return len(p.Specialties) > 0 }

// Level — карьерный уровень по профилю (R1–R4, как в EURAXESS): без степени R1, кандидат наук R2, доктор наук R3,
// доктор наук с учёным званием профессора R4. Это оценка для подсказок, а не утверждение о человеке.
func (p Profile) Level() int {
	switch {
	case degreeRank[p.Degree] >= 2 && titleRank[p.Title] >= 2:
		return 4
	case degreeRank[p.Degree] >= 2:
		return 3
	case degreeRank[p.Degree] == 1:
		return 2
	}
	return 1
}

// Groups — группы специальностей (первые два числа кода, 1.4), в которых у человека есть специальности.
func (p Profile) Groups() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range p.Specialties {
		parts := strings.Split(c, ".")
		if len(parts) < 2 {
			continue
		}
		g := parts[0] + "." + parts[1]
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

// commonSegments — сколько первых частей кода совпало: 1.4.4 и 1.4.2 дают 2.
func commonSegments(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

func fieldPoints(have, want []string) (int, string) {
	best := 0
	for _, a := range have {
		for _, b := range want {
			best = max(best, commonSegments(a, b))
		}
	}
	switch {
	case best >= 3:
		return pointsSpecialty, ReasonSpecialty
	case best == 2:
		return pointsGroup, ReasonGroup
	}
	return 0, ""
}

// Score оценивает, насколько вакансия подходит человеку. Второе значение false: вакансия не подходит вовсе (нет общей
// группы специальностей, степени или звания не хватает), показывать её в подборке нельзя.
func Score(p Profile, o Opening) (Match, bool) {
	field, reason := fieldPoints(p.Specialties, o.Specialties)
	if field == 0 {
		return Match{}, false
	}
	need, have := degreeRank[o.Degree], degreeRank[p.Degree]
	if have < need || titleRank[p.Title] < titleRank[o.Title] {
		return Match{}, false
	}
	m := Match{Score: field, Reasons: []string{reason}}
	if o.CareerLevel != nil {
		switch diff := *o.CareerLevel - p.Level(); {
		case diff == 0:
			m.Score += pointsLevel
			m.Reasons = append(m.Reasons, ReasonLevel)
		case diff == 1 || diff == -1:
			m.Score += pointsLevelNear
			m.Reasons = append(m.Reasons, ReasonLevelNear)
		}
	}
	if need > 0 {
		pts := pointsDegreeAbv
		if have == need {
			pts = pointsDegree
		}
		m.Score += pts
		m.Reasons = append(m.Reasons, ReasonDegree)
	}
	switch {
	case p.Region != "" && p.Region == o.Region:
		m.Score += pointsRegion
		m.Reasons = append(m.Reasons, ReasonRegion)
	case o.Format == "remote":
		m.Score += pointsRemote
		m.Reasons = append(m.Reasons, ReasonRemote)
	}
	return m, true
}
