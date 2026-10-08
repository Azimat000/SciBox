package seed

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"unicode"
	"unicode/utf8"

	"scibox/server/internal/access"
	"scibox/server/internal/orgs"
	"scibox/server/internal/vacancies"
)

// Демонстрационные данные для поиска (срез 6): ещё 22 организации и около 175 вакансий. Организации и подразделения
// написаны вручную; вакансии собираются из направлений (profiles.go) по должностям, поэтому тексты связные, но
// однотипные: это заполнитель, не образец стиля. Всё вымышленное, города настоящие (D-022). Генератор не использует
// случайности извне: одно и то же название организации даёт один и тот же набор вакансий.

type genUnit struct {
	name, kind string
	profile    int
}

type genOrg struct {
	slug, name, kind, city, region, website, description string
	owner, hr                                            string // ключи людей из people
	units                                                []genUnit
	count                                                int // сколько вакансий
}

var generatedOrgs = []genOrg{
	{"moskovskiy-institut-prikladnoy-matematiki", "Московский институт прикладной математики", orgs.KindInstitute, "Москва", "77", "https://mipm.example.ru",
		"Институт занимается вычислительной математикой, искусственным интеллектом и их применением в физике и биологии. Есть собственный вычислительный кластер.",
		"sokolov", "nikitin", []genUnit{{"Лаборатория машинного обучения", orgs.UnitLaboratory, pAI}, {"Отдел численных методов", orgs.UnitDivision, pMath}}, 10},
	{"uralskiy-universitet-materialovedeniya", "Уральский университет материаловедения", orgs.KindUniversity, "Екатеринбург", "66", "https://umat.example.ru",
		"Университет готовит инженеров и исследователей для металлургии и машиностроения; кафедры работают по договорам с заводами региона.",
		"belov", "kuznetsova", []genUnit{{"Кафедра металловедения", orgs.UnitDepartment, pMaterials}, {"Лаборатория аддитивных технологий", orgs.UnitLaboratory, pMaterials}, {"Кафедра физики твёрдого тела", orgs.UnitDepartment, pCondensed}}, 11},
	{"krasnodarskiy-institut-genomiki-rasteniy", "Краснодарский институт геномики растений", orgs.KindInstitute, "Краснодар", "23", "https://kigr.example.ru",
		"Расшифровываем геномы зерновых и плодовых культур и выводим новые сорта для юга России.",
		"fedorov", "gromova", []genUnit{{"Лаборатория геномики культур", orgs.UnitLaboratory, pGenetics}, {"Отдел селекции", orgs.UnitDivision, pAgro}}, 9},
	{"tomskiy-tsentr-fotoniki", "Томский центр фотоники и лазеров", orgs.KindScienceCenter, "Томск", "70", "https://tomsk-photonics.example.ru",
		"Лазерные источники, волоконная оптика и оптические сенсоры. Центр сотрудничает с университетами Томска и промышленными заказчиками.",
		"nikitin", "sokolov", []genUnit{{"Лаборатория лазерной спектроскопии", orgs.UnitLaboratory, pPhotonics}, {"Отдел волоконной оптики", orgs.UnitDivision, pPhotonics}}, 8},
	{"baykalskiy-institut-ekologii-i-klimata", "Байкальский институт экологии и климата", orgs.KindInstitute, "Иркутск", "38", "https://bikk.example.ru",
		"Изучаем озеро Байкал и его водосбор: качество воды, биоразнообразие, региональный климат.",
		"kuznetsova", "fedorov", []genUnit{{"Лаборатория водной экологии", orgs.UnitLaboratory, pEcology}, {"Отдел климата и атмосферы", orgs.UnitDivision, pClimate}}, 9},
	{"krasnoyarskiy-universitet-prirodnykh-sistem", "Красноярский университет природных систем", orgs.KindUniversity, "Красноярск", "24", "https://kunps.example.ru",
		"Университет сибирской тайги: экология, лесоведение, науки о Земле. Студенты проходят практику в заповедниках.",
		"gromova", "belov", []genUnit{{"Кафедра экологии и природопользования", orgs.UnitDepartment, pEcology}, {"Лаборатория дистанционного зондирования", orgs.UnitLaboratory, pClimate}, {"Кафедра геоэкологии", orgs.UnitDepartment, pGeology}}, 10},
	{"dalnevostochnyy-institut-okeanologii", "Дальневосточный институт океанологии «Залив»", orgs.KindInstitute, "Владивосток", "25", "https://zaliv-ocean.example.ru",
		"Океанографические рейсы в Японском и Охотском морях, моделирование течений и экосистем шельфа.",
		"zaitseva", "orlova", []genUnit{{"Лаборатория морских течений", orgs.UnitLaboratory, pOcean}, {"Лаборатория фитопланктона", orgs.UnitLaboratory, pOcean}}, 8},
	{"baltiyskiy-nauchno-tekhnicheskiy-universitet", "Балтийский научно-технический университет", orgs.KindUniversity, "Калининград", "39", "https://bntu.example.ru",
		"Технический университет на побережье: радиофизика, электроника, робототехника для морской отрасли.",
		"nikitin", "gromova", []genUnit{{"Кафедра радиофизики", orgs.UnitDepartment, pElectronics}, {"Кафедра робототехники и управления", orgs.UnitDepartment, pControl}}, 9},
	{"permskiy-institut-neftegazovoy-geologii", "Пермский институт нефтегазовой геологии", orgs.KindInstitute, "Пермь", "59", "https://pingg.example.ru",
		"Геология осадочных бассейнов Урала и Предуралья, гидрогеология и геофизика месторождений.",
		"belov", "sokolov", []genUnit{{"Отдел геологии бассейнов", orgs.UnitDivision, pGeology}, {"Лаборатория геофизики", orgs.UnitLaboratory, pClimate}}, 7},
	{"saratovskiy-universitet-biomeditsiny", "Саратовский университет биомедицины", orgs.KindUniversity, "Саратов", "64", "https://subm.example.ru",
		"Медико-биологический университет: клеточные технологии, иммунология, доклинические исследования.",
		"fedorov", "kuznetsova", []genUnit{{"Лаборатория клеточных технологий", orgs.UnitLaboratory, pBiomed}, {"Кафедра иммунологии", orgs.UnitDepartment, pBiomed}, {"Лаборатория молекулярной генетики", orgs.UnitLaboratory, pGenetics}}, 10},
	{"voronezhskiy-agrouniversitet", "Воронежский аграрный университет", orgs.KindUniversity, "Воронеж", "36", "https://vgau.example.ru",
		"Агрономия, селекция и почвоведение для чернозёмных регионов. Опытные поля площадью около двух тысяч гектаров.",
		"gromova", "belov", []genUnit{{"Кафедра растениеводства", orgs.UnitDepartment, pAgro}, {"Лаборатория почвенного плодородия", orgs.UnitLaboratory, pAgro}}, 8},
	{"yuzhno-uralskiy-tsentr-materialovedeniya", "Южно-Уральский центр материаловедения", orgs.KindScienceCenter, "Челябинск", "74", "https://yumc.example.ru",
		"Испытания и разработка конструкционных материалов совместно с предприятиями Южного Урала.",
		"sokolov", "orlova", []genUnit{{"Лаборатория механических испытаний", orgs.UnitLaboratory, pMaterials}, {"Отдел физики конденсированного состояния", orgs.UnitDivision, pCondensed}}, 7},
	{"rostovskiy-institut-sotsialno-ekonomicheskikh-issledovaniy", "Ростовский институт социально-экономических исследований", orgs.KindInstitute, "Ростов-на-Дону", "61", "https://risei.example.ru",
		"Региональная экономика, рынки труда и образование юга России. Данные и аналитика для региональных властей.",
		"kuznetsova", "gromova", []genUnit{{"Отдел региональной экономики", orgs.UnitDivision, pEconomics}, {"Лаборатория образовательной аналитики", orgs.UnitLaboratory, pPsychology}, {"Отдел гуманитарных исследований", orgs.UnitDivision, pHumanities}}, 8},
	{"samarskiy-universitet-aviatsionnykh-tekhnologiy", "Самарский университет авиационных технологий", orgs.KindUniversity, "Самара", "63", "https://suat.example.ru",
		"Авиационные двигатели, материалы и системы управления. Крупные договоры с предприятиями отрасли.",
		"nikitin", "fedorov", []genUnit{{"Кафедра систем управления", orgs.UnitDepartment, pControl}, {"Лаборатория композиционных материалов", orgs.UnitLaboratory, pMaterials}}, 9},
	{"ufimskiy-institut-khimii-kataliza", "Уфимский институт химии и катализа", orgs.KindInstitute, "Уфа", "02", "https://uikk.example.ru",
		"Катализ, нефтехимия и органический синтез. Лаборатории оснащены спектрометрами ЯМР и хромато-масс-спектрометрами.",
		"orlova", "kuznetsova", []genUnit{{"Лаборатория органического синтеза", orgs.UnitLaboratory, pOrganic}, {"Лаборатория катализа", orgs.UnitLaboratory, pPhysChem}}, 9},
	{"tyumenskiy-universitet-arktiki-i-energetiki", "Тюменский университет Арктики и энергетики", orgs.KindUniversity, "Тюмень", "72", "https://tuae.example.ru",
		"Геология и климат северных территорий, энергетика и устойчивое развитие.",
		"zaitseva", "belov", []genUnit{{"Кафедра геологии и геокриологии", orgs.UnitDepartment, pGeology}, {"Лаборатория климата Севера", orgs.UnitLaboratory, pClimate}}, 8},
	{"tekhnopark-uralskiy-start", "Технопарк «Уральский старт»", orgs.KindTechnopark, "Екатеринбург", "66", "https://ural-start.example.ru",
		"Площадка для научных стартапов: акселератор, лаборатории коллективного пользования и сопровождение грантов.",
		"gromova", "sokolov", []genUnit{{"Центр сопровождения грантов", orgs.UnitDivision, pEconomics}, {"ЦКП «Приборная база»", orgs.UnitSharedFacility, pMaterials}}, 7},
	{"pushchinskiy-institut-kletochnoy-biologii", "Пущинский институт клеточной биологии", orgs.KindInstitute, "Пущино", "50", "https://pikb.example.ru",
		"Клеточная и молекулярная биология, биофизика, нейробиология. Институт расположен в научном городке под Москвой.",
		"fedorov", "orlova", []genUnit{{"Лаборатория клеточной биологии", orgs.UnitLaboratory, pBiomed}, {"Лаборатория молекулярной биологии", orgs.UnitLaboratory, pGenetics}, {"Отдел биоинформатики", orgs.UnitDivision, pBioinfo}}, 11},
	{"protvinskiy-institut-uskoritelnykh-tekhnologiy", "Протвинский институт ускорительных технологий", orgs.KindInstitute, "Протвино", "50", "https://piut.example.ru",
		"Ускорители частиц, полупроводниковые детекторы и методы диагностики пучков.",
		"belov", "nikitin", []genUnit{{"Отдел детекторов", orgs.UnitDivision, pElectronics}, {"Лаборатория физики пучков", orgs.UnitLaboratory, pCondensed}}, 8},
	{"severo-vostochnyy-institut-merzlotovedeniya", "Северо-Восточный институт мерзлотоведения", orgs.KindInstitute, "Якутск", "14", "https://sevi.example.ru",
		"Вечная мерзлота, её изменения и последствия для инфраструктуры и экосистем Якутии.",
		"kuznetsova", "fedorov", []genUnit{{"Лаборатория мерзлотных процессов", orgs.UnitLaboratory, pGeology}, {"Отдел климатических изменений", orgs.UnitDivision, pClimate}}, 8},
	{"murmanskiy-arkticheskiy-universitet", "Мурманский арктический университет", orgs.KindUniversity, "Мурманск", "51", "https://mau-arctic.example.ru",
		"Морские науки, рыбное хозяйство и экология Баренцева моря. Своё научное судно и морская станция.",
		"zaitseva", "gromova", []genUnit{{"Кафедра океанологии", orgs.UnitDepartment, pOcean}, {"Лаборатория экологии шельфа", orgs.UnitLaboratory, pEcology}}, 9},
	{"lazernye-sistemy-neva", "Лазерные системы «Нева»", orgs.KindRDCompany, "Санкт-Петербург", "78", "https://neva-lasers.example.ru",
		"Компания разрабатывает лазерные источники и оптические сенсоры для промышленности и науки; собственная научная лаборатория.",
		"nikitin", "sokolov", []genUnit{{"Исследовательская лаборатория лазеров", orgs.UnitLaboratory, pPhotonics}, {"Отдел электроники", orgs.UnitDivision, pElectronics}}, 7},
}

// Цели и порядок: должности по типам организаций. Повтор в списке — вес.
var positionsByKind = map[string][]string{
	orgs.KindUniversity: {"docent", "docent", "docent", "senior_lecturer", "senior_lecturer", "lecturer", "assistant", "assistant", "professor",
		"department_head", "phd_student", "phd_student", "master_student", "postdoc", "senior_researcher", "researcher", "junior_researcher", "research_engineer",
		"methodist", "student_intern"},
	orgs.KindInstitute: {"junior_researcher", "junior_researcher", "researcher", "researcher", "senior_researcher", "senior_researcher", "leading_researcher",
		"chief_researcher", "lab_head", "research_engineer", "research_engineer", "research_lab_assistant", "phd_student", "phd_student", "postdoc", "postdoc", "master_student", "intern_researcher"},
	orgs.KindScienceCenter: {"junior_researcher", "researcher", "researcher", "senior_researcher", "senior_researcher", "leading_researcher", "lab_head",
		"research_engineer", "research_engineer", "phd_student", "postdoc", "postdoc", "intern_researcher"},
	orgs.KindRDCompany:  {"researcher", "researcher", "senior_researcher", "research_engineer", "research_engineer", "research_engineer", "intern_researcher", "postdoc", "lab_head", "project_specialist"},
	orgs.KindTechnopark: {"grant_manager", "grant_manager", "tech_transfer", "tech_transfer", "shared_facility_head", "research_engineer", "intern_researcher", "project_specialist", "grant_executor"},
}

type positionInfo struct {
	name, genitive string
	level          int
	degrees        []string // допустимые степени, берётся одна
	salary         [2]int   // от и до, тысяч рублей в месяц (Москва и Петербург дороже)
	teaching       bool
}

var positionInfos = map[string]positionInfo{
	"research_lab_assistant": {"Лаборант-исследователь", "лаборанта-исследователя", 1, []string{"none"}, [2]int{40, 55}, false},
	"junior_researcher":      {"Младший научный сотрудник", "младшего научного сотрудника", 1, []string{"none", "none", "candidate"}, [2]int{55, 75}, false},
	"researcher":             {"Научный сотрудник", "научного сотрудника", 2, []string{"none", "candidate"}, [2]int{70, 95}, false},
	"senior_researcher":      {"Старший научный сотрудник", "старшего научного сотрудника", 3, []string{"candidate"}, [2]int{90, 125}, false},
	"leading_researcher":     {"Ведущий научный сотрудник", "ведущего научного сотрудника", 3, []string{"candidate", "doctor"}, [2]int{120, 160}, false},
	"chief_researcher":       {"Главный научный сотрудник", "главного научного сотрудника", 4, []string{"doctor"}, [2]int{150, 210}, false},
	"lab_head":               {"Заведующий лабораторией", "заведующего лабораторией", 4, []string{"candidate", "doctor"}, [2]int{140, 190}, false},
	"research_engineer":      {"Инженер-исследователь", "инженера-исследователя", 2, []string{"none", "candidate"}, [2]int{65, 95}, false},
	"assistant":              {"Ассистент", "ассистента", 1, []string{"none"}, [2]int{45, 65}, true},
	"lecturer":               {"Преподаватель", "преподавателя", 2, []string{"none", "candidate"}, [2]int{55, 80}, true},
	"senior_lecturer":        {"Старший преподаватель", "старшего преподавателя", 2, []string{"candidate"}, [2]int{65, 90}, true},
	"docent":                 {"Доцент", "доцента", 3, []string{"candidate"}, [2]int{80, 115}, true},
	"professor":              {"Профессор", "профессора", 4, []string{"doctor"}, [2]int{120, 170}, true},
	"department_head":        {"Заведующий кафедрой", "заведующего кафедрой", 4, []string{"doctor"}, [2]int{130, 180}, true},
	"phd_student":            {"Аспирантура", "", 1, []string{"none"}, [2]int{35, 55}, false},
	"master_student":         {"Магистратура", "", 1, []string{"none"}, [2]int{15, 30}, false},
	"postdoc":                {"Постдок", "", 2, []string{"candidate"}, [2]int{100, 150}, false},
	"intern_researcher":      {"Стажёр-исследователь", "", 1, []string{"none"}, [2]int{20, 35}, false},
	"grant_manager":          {"Грант-менеджер научных проектов", "", 0, []string{"none"}, [2]int{80, 120}, false},
	"tech_transfer":          {"Специалист по трансферу технологий", "", 0, []string{"none"}, [2]int{90, 130}, false},
	"shared_facility_head":   {"Руководитель ЦКП", "", 0, []string{"candidate"}, [2]int{120, 170}, false},
	"methodist":              {"Методист", "", 0, []string{"none"}, [2]int{45, 65}, false},
	"student_intern":         {"Стажировка для студентов", "", 0, []string{"none"}, [2]int{15, 25}, false},
	"project_specialist":     {"Специалист проекта", "", 0, []string{"none"}, [2]int{70, 110}, false},
	"grant_executor":         {"Исполнитель по гранту", "", 0, []string{"none", "candidate"}, [2]int{50, 90}, false},
}

// cityFactor — во сколько раз зарплата в городе выше, чем в среднем по списку.
var cityFactor = map[string]float64{"Москва": 1.4, "Санкт-Петербург": 1.3, "Екатеринбург": 1.1, "Якутск": 1.25, "Мурманск": 1.15, "Пущино": 1.1, "Протвино": 1.1}

var researchCodes = map[string]bool{"research_lab_assistant": true, "junior_researcher": true, "researcher": true, "senior_researcher": true,
	"leading_researcher": true, "chief_researcher": true, "lab_head": true, "research_engineer": true}

// unitFor выбирает подразделение для вакансии: должности ППС идут на кафедры, заведующий лабораторией в лабораторию,
// руководитель ЦКП в ЦКП, грант-менеджеры и трансфер технологий в остальные подразделения.
func unitFor(o genOrg, code string, i int) int {
	var match func(kind string) bool
	switch {
	case positionInfos[code].teaching:
		match = func(k string) bool { return k == orgs.UnitDepartment }
	case code == "lab_head":
		match = func(k string) bool { return k == orgs.UnitLaboratory }
	case code == "shared_facility_head":
		match = func(k string) bool { return k == orgs.UnitSharedFacility }
	case code == "methodist":
		match = func(k string) bool { return k == orgs.UnitDepartment }
	case code == "grant_manager" || code == "tech_transfer":
		match = func(k string) bool { return k != orgs.UnitSharedFacility }
	default:
		return i % len(o.units)
	}
	var idx []int
	for n, u := range o.units {
		if match(u.kind) {
			idx = append(idx, n)
		}
	}
	if len(idx) == 0 {
		return i % len(o.units)
	}
	return idx[i%len(idx)]
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func pick[T any](rng *rand.Rand, items []T) T { return items[rng.IntN(len(items))] }

func chance(rng *rand.Rand, percent int) bool { return rng.IntN(100) < percent }

func seedFor(slug string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(slug))
	return rand.New(rand.NewPCG(h.Sum64(), 2026)) //nolint:gosec // G404: демо-данные должны быть одинаковыми при каждом запуске, тайны здесь нет
}

// roundTo округляет рубли до пяти тысяч.
func roundTo(v float64) int { return int(v/5000+0.5) * 5000 }

// generatedVacancies собирает вакансии одной организации.
func generatedVacancies(o genOrg) []vacSeed {
	rng := seedFor(o.slug)
	out := make([]vacSeed, 0, o.count)
	seen := map[string]bool{}
	for i := 0; i < o.count; i++ {
		// Одинаковых названий в одной организации не бывает: пробуем ещё раз, а если не вышло, пропускаем.
		for try := 0; try < 40; try++ {
			v := generatedVacancy(o, rng, i)
			if !seen[v.title] {
				seen[v.title] = true
				out = append(out, v)
				break
			}
		}
	}
	return out
}

func generatedVacancy(o genOrg, rng *rand.Rand, i int) vacSeed {
	code := pick(rng, positionsByKind[o.kind])
	info := positionInfos[code]
	unitIdx := unitFor(o, code, i)
	unit := o.units[unitIdx]
	p := profiles[unit.profile]
	topic := pick(rng, p.topics)
	subject := o.name
	if unit.name != "" {
		subject = unit.name
	}

	v := vacSeed{unit: unitIdx, position: code, region: o.region, city: o.city, specialties: p.specialties}
	v.degree = pick(rng, info.degrees)
	v.level = info.level

	// Формат: теоретические направления иногда удалённо.
	switch {
	case p.remote && chance(rng, 25):
		v.format, v.region, v.city = vacancies.FormatRemote, "", ""
	case chance(rng, 25):
		v.format = vacancies.FormatHybrid
	default:
		v.format = vacancies.FormatOnsite
	}
	remote := v.format == vacancies.FormatRemote

	// Тип позиции определяет поля.
	switch {
	case info.teaching:
		discipline := pick(rng, p.disciplines)
		v.title = fmt.Sprintf("%s: %s", info.name, strings.ToLower(discipline))
		v.focus = discipline
		switch rng.IntN(2) {
		case 0:
			v.summary = fmt.Sprintf("%s объявляет набор на должность %s. Дисциплина — «%s».", subject, info.genitive, discipline)
		default:
			v.summary = fmt.Sprintf("%s ищет %s для преподавания дисциплины «%s» и участия в научной работе подразделения.", subject, info.genitive, discipline)
		}
		v.description = fmt.Sprintf("Нагрузка: лекции и практические занятия по дисциплине «%s», руководство курсовыми и выпускными работами; остальное время — научная работа в подразделении. %s", discipline, conditions(v.format))
		v.rate, v.contract = pickRate(rng, 60), contractFor(rng, 55)
		v.funding = pick(rng, []string{vacancies.FundingBudget, vacancies.FundingBudget, vacancies.FundingContract})
		switch {
		case code == "professor" || code == "department_head":
			v.academicTitle = vacancies.TitleProfessor
		case code == "docent" && chance(rng, 30):
			v.academicTitle = vacancies.TitleDocent
		}
	case code == "phd_student" || code == "master_student" || code == "intern_researcher" || code == "student_intern":
		v.title = fmt.Sprintf("%s: %s", info.name, topic)
		v.focus = capitalize(topic)
		switch code {
		case "phd_student":
			v.summary = fmt.Sprintf("%s открывает место в аспирантуре по теме «%s».", subject, topic)
			v.contract, v.months, v.funding = vacancies.ContractFixed, pick(rng, []int{36, 48}), vacancies.FundingBudget
		case "master_student":
			v.summary = fmt.Sprintf("%s принимает магистрантов на тему «%s».", subject, topic)
			v.contract, v.months, v.funding = vacancies.ContractFixed, 24, vacancies.FundingBudget
		case "student_intern":
			v.summary = fmt.Sprintf("%s берёт студентов на стажировку по теме «%s».", subject, topic)
			v.contract, v.months, v.funding = vacancies.ContractFixed, pick(rng, []int{1, 2, 3}), vacancies.FundingOwn
		default:
			v.summary = fmt.Sprintf("%s принимает стажёров-исследователей на тему «%s».", subject, topic)
			v.contract, v.months, v.funding = vacancies.ContractFixed, pick(rng, []int{3, 6, 12}), vacancies.FundingOwn
		}
		v.description = fmt.Sprintf("Вы будете работать над темой «%s» под руководством опытного исследователя, участвовать в семинарах подразделения и готовить публикации. %s %s", topic, p.work, conditions(v.format))
	case code == "project_specialist" || code == "grant_executor":
		v.level, v.contract, v.months = 0, vacancies.ContractFixed, pick(rng, []int{6, 12, 24})
		v.funding = pick(rng, []string{vacancies.FundingGrant, vacancies.FundingContract})
		v.rate = pickRate(rng, 50)
		v.title = fmt.Sprintf("%s: %s", info.name, topic)
		v.focus = capitalize(topic)
		v.summary = fmt.Sprintf("%s набирает людей в проект «%s» на срок проекта.", subject, topic)
		v.description = fmt.Sprintf("Работа в проектной группе: эксперименты, обработка данных, отчёты заказчику или фонду. %s", conditions(v.format))
	case code == "grant_manager" || code == "tech_transfer" || code == "shared_facility_head" || code == "methodist":
		v.level, v.contract, v.funding = 0, vacancies.ContractPermanent, pick(rng, []string{vacancies.FundingBudget, vacancies.FundingOwn})
		v.rate = pickRate(rng, 15)
		v.focus = map[string]string{"grant_manager": "Сопровождение научных проектов", "tech_transfer": "Трансфер технологий", "shared_facility_head": "Руководство центром коллективного пользования", "methodist": "Учебно-методическая работа"}[code]
		v.title = info.name
		switch code {
		case "methodist":
			v.title = fmt.Sprintf("%s: %s", info.name, p.name)
			v.summary = fmt.Sprintf("%s ищет методиста: расписание, учебные планы, документы для студентов и преподавателей.", subject)
			v.description = "Вы будете составлять расписание и учебные планы, готовить документы к аккредитации, помогать студентам и преподавателям с учебными вопросами."
		case "grant_manager":
			v.title = fmt.Sprintf("%s: %s", info.name, p.name)
			v.summary = fmt.Sprintf("%s ищет грант-менеджера для сопровождения проектов в области «%s».", subject, p.name)
			v.description = "Вы будете помогать учёным готовить заявки на гранты, вести отчётность и сроки, следить за конкурсами фондов. Знание правил РНФ и Минобрнауки — плюс."
		case "tech_transfer":
			if chance(rng, 50) {
				v.title = fmt.Sprintf("%s: %s", info.name, p.name)
			}
			v.summary = fmt.Sprintf("%s ищет специалиста по трансферу технологий: патенты, лицензии, работа с учёными и компаниями.", subject)
			v.description = "Вы будете оформлять права на результаты интеллектуальной деятельности, искать партнёров в промышленности и сопровождать лицензионные договоры."
		default:
			v.title = info.name + ": " + p.name
			v.summary = fmt.Sprintf("%s ищет руководителя: график приборов, закупки, обучение пользователей, отчётность.", subject)
			v.description = "Вы будете отвечать за график работы приборов, закупки расходных материалов, обучение пользователей и отчётность перед учредителем."
		}
		v.description += " " + conditions(v.format)
		if remote {
			// Управленческая позиция редко удалённая: возвращаем очную работу.
			v.format, v.region, v.city = vacancies.FormatOnsite, o.region, o.city
		}
	default: // научные должности
		v.title = fmt.Sprintf("%s: %s", info.name, topic)
		v.focus = capitalize(topic)
		switch rng.IntN(3) {
		case 0:
			v.summary = fmt.Sprintf("%s ищет сотрудника для работы по теме «%s».", subject, topic)
		case 1:
			v.summary = fmt.Sprintf("%s расширяет проект по направлению «%s» и приглашает исследователя в команду.", subject, topic)
		default:
			v.summary = fmt.Sprintf("Тема проекта — «%s». Подразделение: %s.", topic, subject)
		}
		v.description = fmt.Sprintf("%s Работа идёт в составе небольшой группы, результаты публикуются в рецензируемых журналах. %s", p.work, conditions(v.format))
		v.rate, v.contract = pickRate(rng, 15), contractFor(rng, 40)
		v.funding = pick(rng, []string{vacancies.FundingGrant, vacancies.FundingGrant, vacancies.FundingBudget, vacancies.FundingBudget, vacancies.FundingContract, vacancies.FundingOwn})
	}
	if o.kind == orgs.KindRDCompany && v.funding == vacancies.FundingBudget {
		v.funding = vacancies.FundingContract
	}
	if v.rate == 0 && v.level != 0 && code != "phd_student" && code != "master_student" && code != "intern_researcher" {
		v.rate = 100
	}
	if v.contract == vacancies.ContractFixed && v.months == 0 {
		v.months = pick(rng, []int{12, 24, 36, 60})
	}
	if v.funding == vacancies.FundingGrant {
		v.note = fmt.Sprintf("Грант РНФ %d-%d-%05d", 23+rng.IntN(3), 11+rng.IntN(5), rng.IntN(99999))
	}

	// Зарплата или стипендия: у части вакансий не указана (D-014), у части только «от».
	base := info.salary
	factor := 1.0
	if f, ok := cityFactor[o.city]; ok {
		factor = f
	}
	show := 70
	if code == "phd_student" || code == "master_student" || code == "intern_researcher" || code == "student_intern" || code == "postdoc" {
		show = 90
	}
	if chance(rng, show) {
		lo := roundTo(float64(base[0]*1000) * factor)
		hi := roundTo(float64(base[1]*1000) * factor)
		v.salaryFrom = lo
		if chance(rng, 70) && hi > lo {
			v.salaryTo = hi
		}
	}

	// Жильё и срок подачи.
	switch {
	case !remote && (code == "phd_student" || code == "master_student" || code == "intern_researcher" || code == "student_intern") && chance(rng, 55):
		v.housing = vacancies.HousingDormitory
	case !remote && chance(rng, 18):
		v.housing = pick(rng, []string{vacancies.HousingService, vacancies.HousingCompensation})
	}
	if chance(rng, 65) {
		v.daysLeft = 3 + rng.IntN(70)
	}
	canCompete := info.teaching || researchCodes[code]
	if canCompete && info.teaching && v.daysLeft == 0 {
		v.daysLeft = 20 + rng.IntN(40) // конкурс на должность ППС всегда со сроком
	}
	if canCompete && v.daysLeft > 0 && chance(rng, 45) {
		v.competition = true
	}

	// Требования.
	v.requirements = requirements(v.degree, v.academicTitle, code, p.skills)

	// Статусы: немного закрытых, один-два черновика и пара вакансий с уже прошедшим сроком (поиск их прячет).
	switch {
	case i == o.count-1 && chance(rng, 20):
		v.status = vacancies.StatusDraft
	case chance(rng, 5):
		v.status = vacancies.StatusClosed
	case chance(rng, 4) && v.daysLeft > 0:
		v.daysLeft = -(1 + rng.IntN(20))
		v.competition = false
	}
	return v
}

func conditions(format string) string {
	switch format {
	case vacancies.FormatHybrid:
		return "Часть недели можно работать удалённо."
	case vacancies.FormatRemote:
		return "Работа удалённая, встречи с группой проходят по расписанию."
	}
	return "Работа очная, в помещениях организации."
}

func pickRate(rng *rand.Rand, partPercent int) int {
	if chance(rng, partPercent) {
		return pick(rng, []int{50, 75})
	}
	return 100
}

func contractFor(rng *rand.Rand, permanentPercent int) string {
	if chance(rng, permanentPercent) {
		return vacancies.ContractPermanent
	}
	return vacancies.ContractFixed
}

func requirements(degree, title, code, skills string) string {
	var parts []string
	switch degree {
	case vacancies.DegreeCandidate:
		parts = append(parts, "Степень кандидата наук.")
	case vacancies.DegreeDoctor:
		parts = append(parts, "Степень доктора наук.")
	}
	switch code {
	case "senior_researcher", "leading_researcher", "chief_researcher", "lab_head":
		parts = append(parts, "Не менее пяти публикаций в рецензируемых журналах за последние пять лет.")
	case "docent", "professor", "department_head", "senior_lecturer":
		parts = append(parts, "Опыт преподавания в вузе не менее трёх лет.")
	case "phd_student":
		parts = append(parts, "Диплом специалиста или магистра по профильному направлению.")
	case "master_student":
		parts = append(parts, "Диплом бакалавра или специалиста.")
	case "student_intern":
		return "Студенты старших курсов профильных направлений. " + skills
	case "methodist":
		return "Высшее образование, уверенное владение компьютером (Word, Excel), аккуратность в документах."
	case "grant_manager", "tech_transfer", "shared_facility_head":
		return "Опыт работы в научной организации или университете не менее двух лет. Умение вести документы и сроки."
	}
	if title == vacancies.TitleProfessor {
		parts = append(parts, "Учёное звание профессора.")
	}
	parts = append(parts, skills)
	return strings.Join(parts, " ")
}

// init добавляет сгенерированные организации и их вакансии к ручным.
func init() {
	for _, g := range generatedOrgs {
		o := orgSeed{
			slug: g.slug, name: g.name, kind: g.kind, city: g.city, website: g.website, description: g.description,
			members: []memberSeed{{g.owner, access.RoleOwner}, {g.hr, access.RoleHR}},
		}
		for _, u := range g.units {
			p := profiles[u.profile]
			topics := make([]string, 0, 3)
			for _, t := range p.topics[:3] {
				topics = append(topics, capitalize(t))
			}
			o.units = append(o.units, unitSeed{name: u.name, kind: u.kind, description: unitDescription(u, p), topics: topics})
		}
		organizations = append(organizations, o)
		vacanciesByOrg[g.slug] = generatedVacancies(g)
	}
}

func unitDescription(u genUnit, p profile) string {
	switch u.kind {
	case orgs.UnitDepartment:
		return fmt.Sprintf("Кафедра ведёт обучение и научную работу в области: %s.", p.name)
	case orgs.UnitSharedFacility:
		return fmt.Sprintf("Центр коллективного пользования: оборудование и методы по направлению «%s», доступны внешним группам.", p.name)
	case orgs.UnitDivision:
		return fmt.Sprintf("Отдел занимается темами: %s, %s.", p.topics[0], p.topics[1])
	}
	return fmt.Sprintf("Лаборатория работает по направлениям: %s, %s, %s.", p.topics[0], p.topics[1], p.topics[2])
}
