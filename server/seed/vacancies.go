package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/vacancies"
)

// vacSeed — демонстрационная вакансия. Сроки подачи считаются от дня загрузки данных (daysLeft).
type vacSeed struct {
	unit                  int    // номер подразделения организации; -1 — вакансия на всю организацию
	position, title       string // код должности из справочника
	summary, description  string
	requirements, focus   string
	level                 int // 1–4 (R1–R4); 0 — не указан
	format, region, city  string
	housing               string
	rate                  int // процент ставки; 0 — не указана
	salaryFrom, salaryTo  int // 0 — не указана
	contract              string
	months                int
	funding, note         string
	degree, academicTitle string
	competition           bool
	daysLeft              int // дней до срока подачи; 0 — срока нет
	specialties           []string
	status                string // пусто — опубликована
}

const (
	nsk, kzn, arh, spb, tms, sev, vvo, nnv = "54", "16", "29", "78", "70", "92", "25", "52"
)

var vacanciesByOrg = map[string][]vacSeed{
	"sibirskiy-institut-kvantovykh-materialov": {
		{unit: 0, position: "senior_researcher", title: "Старший научный сотрудник: сверхпроводящие плёнки",
			summary:      "Рост и измерение эпитаксиальных плёнок купратов и пниктидов, работа с установкой молекулярно-лучевой эпитаксии.",
			description:  "Лаборатория ведёт четыре проекта по сверхпроводящим плёнкам. Вы будете руководить ростом образцов, настраивать режимы установки, работать с аспирантами и готовить публикации. Измерения проводятся в криостатах института, доступ к синхротрону по заявкам.",
			requirements: "Кандидат физико-математических наук, опыт эпитаксии или магнитотранспортных измерений, не менее пяти публикаций за последние пять лет.",
			focus:        "Рост и характеризация плёнок", level: 3, format: "onsite", region: nsk, city: "Новосибирск", housing: "service", rate: 100,
			salaryFrom: 95000, salaryTo: 130000, contract: "fixed", months: 36, funding: "grant", note: "Грант РНФ, проект 24-12-00184", degree: "candidate",
			competition: true, daysLeft: 18, specialties: []string{"1.3.8", "1.3.10"}},
		{unit: 0, position: "phd_student", title: "Аспирантура: транспорт в топологических сверхпроводниках",
			summary:      "Место в аспирантуре на бюджетной основе с темой по электронному транспорту в топологических сверхпроводниках.",
			description:  "Тема диссертации: эффекты ближнего порядка в гибридных структурах сверхпроводник — топологический изолятор. Научный руководитель — заведующая лабораторией. Стипендия аспиранта плюс надбавка из гранта.",
			requirements: "Диплом специалиста или магистра по физике, интерес к экспериментальной физике низких температур.",
			focus:        "Транспорт в гибридных структурах", level: 1, format: "onsite", region: nsk, city: "Новосибирск", housing: "dormitory",
			salaryFrom: 40000, contract: "fixed", months: 48, funding: "budget", daysLeft: 45, specialties: []string{"1.3.8", "1.3.10"}},
		{unit: 1, position: "postdoc", title: "Постдок: расчёты электронной структуры",
			summary:      "Год-два работы над расчётами электронной структуры квантовых материалов и методами машинного обучения для потенциалов.",
			description:  "Вы будете развивать собственные коды на основе теории функционала плотности, обучать машинные потенциалы и сопоставлять расчёты с экспериментом соседних лабораторий. Кластер института: 2000 ядер, 16 графических карт.",
			requirements: "Степень PhD или кандидата наук (можно в процессе защиты), опыт работы с VASP, Quantum ESPRESSO или аналогами.",
			focus:        "Теория функционала плотности, машинные потенциалы", level: 2, format: "hybrid", region: nsk, city: "Новосибирск", housing: "compensation",
			salaryFrom: 110000, salaryTo: 140000, contract: "fixed", months: 24, funding: "grant", degree: "candidate", daysLeft: 9,
			specialties: []string{"1.3.8", "1.3.3", "1.2.2"}},
		{unit: 2, position: "research_engineer", title: "Инженер-исследователь на просвечивающий микроскоп",
			summary:      "Обслуживание и развитие просвечивающего электронного микроскопа, помощь пользователям из других институтов.",
			description:  "ЦКП обслуживает двадцать научных групп. Вы будете готовить образцы, проводить съёмку, вести журнал и обучать пользователей. График пятидневный, есть свободный график в пределах недели.",
			requirements: "Высшее образование по физике, химии или материаловедению, опыт работы на электронном микроскопе приветствуется.",
			focus:        "Просвечивающая микроскопия", level: 2, format: "onsite", region: nsk, city: "Новосибирск", rate: 100, salaryFrom: 75000, salaryTo: 100000,
			contract: "permanent", funding: "budget", specialties: []string{"1.3.2", "2.6.17"}},
		{unit: 2, position: "shared_facility_head", title: "Руководитель ЦКП «Электронная микроскопия»",
			summary:     "Управление центром коллективного пользования: график приборов, закупки, отчётность перед министерством.",
			description: "Центр работает с 2012 года, пять приборов, девять сотрудников. Нужен руководитель, который разовьёт центр, выстроит работу с внешними заказчиками и подготовит заявку на обновление оборудования.",
			focus:       "Руководство центром коллективного пользования", format: "onsite", region: nsk, city: "Новосибирск", rate: 100, salaryFrom: 130000, salaryTo: 180000,
			contract: "permanent", funding: "budget", degree: "candidate", daysLeft: 60},
		{unit: -1, position: "junior_researcher", title: "Младший научный сотрудник (черновик)",
			summary: "Черновик вакансии, которую кадровая служба ещё дописывает.", status: vacancies.StatusDraft},
	},
	"privolzhskiy-universitet-tekhnologiy": {
		{unit: 0, position: "docent", title: "Доцент кафедры органической химии (конкурс)",
			summary:      "Лекции и практикум по органической химии для бакалавров, руководство магистерскими работами, участие в грантовых проектах кафедры.",
			description:  "Кафедра объявляет конкурс на должность доцента. Нагрузка около 600 часов в год, остальное время — научная работа. Лаборатория кафедры оснащена спектрометрами ЯМР, ВЭЖХ-МС и газовым хроматографом.",
			requirements: "Кандидат химических наук, опыт преподавания не менее трёх лет, публикации в журналах из перечня ВАК.",
			focus:        "Органическая химия, спецкурс по катализу", level: 3, format: "onsite", region: kzn, city: "Казань", rate: 100, salaryFrom: 70000, salaryTo: 95000,
			contract: "fixed", months: 60, funding: "budget", degree: "candidate", academicTitle: "docent", competition: true, daysLeft: 14,
			specialties: []string{"1.4.3", "1.4.14"}},
		{unit: 0, position: "assistant", title: "Ассистент кафедры органической химии",
			summary:     "Ведение лабораторных занятий по органической химии, подготовка методических материалов, начало научной работы.",
			description: "Подходит выпускникам магистратуры и аспирантам. Нагрузка 0,5 ставки, остальное время — собственная диссертация под руководством сотрудника кафедры.",
			focus:       "Лабораторный практикум по органической химии", level: 1, format: "onsite", region: kzn, city: "Казань", rate: 50, salaryFrom: 30000, salaryTo: 40000,
			contract: "fixed", months: 12, funding: "budget", daysLeft: 30, specialties: []string{"1.4.3"}},
		{unit: 1, position: "professor", title: "Профессор кафедры прикладной математики (конкурс)",
			summary:      "Чтение курсов по оптимизации и численным методам, руководство научной школой, участие в проектах с промышленностью.",
			description:  "Университет ищет профессора, который возглавит направление математического моделирования для промышленных партнёров. Есть договор с тремя предприятиями Татарстана, общий объём заказов около 40 млн рублей в год.",
			requirements: "Доктор физико-математических наук, звание профессора или доцента, не менее десяти публикаций за пять лет, опыт руководства аспирантами.",
			focus:        "Оптимизация, численные методы", level: 4, format: "onsite", region: kzn, city: "Казань", housing: "service", rate: 100, salaryFrom: 150000, salaryTo: 220000,
			contract: "fixed", months: 60, funding: "budget", degree: "doctor", academicTitle: "docent", competition: true, daysLeft: 75,
			specialties: []string{"1.1.6", "1.2.2", "1.1.2"}},
		{unit: 2, position: "researcher", title: "Научный сотрудник: анализ секвенирования",
			summary:      "Разработка конвейеров анализа данных секвенирования нового поколения для клинических проектов.",
			description:  "Лаборатория работает с клиниками региона: экзомы, транскриптомы, онкопанели. Нужен специалист, который автоматизирует анализ и научит коллег пользоваться конвейерами.",
			requirements: "Кандидат наук или магистр с публикациями, Python или R, опыт работы с Nextflow или Snakemake.",
			focus:        "Биоинформатические конвейеры", level: 2, format: "remote", rate: 100, salaryFrom: 90000, salaryTo: 120000, contract: "permanent",
			funding: "contract", specialties: []string{"1.5.8", "1.5.7"}},
		{unit: -1, position: "master_student", title: "Магистратура: биоинформатика и геномика",
			summary:     "Набор на магистерскую программу с лабораторной практикой в научных группах университета.",
			description: "Программа рассчитана на два года, половина времени — работа в лаборатории. Лучшим студентам платится повышенная стипендия из средств гранта.",
			focus:       "Магистерская программа", level: 1, format: "onsite", region: kzn, city: "Казань", housing: "dormitory", contract: "fixed", months: 24,
			funding: "budget", daysLeft: 40, specialties: []string{"1.5.8"}},
	},
	"tsentr-izucheniya-arktiki": {
		{unit: 0, position: "leading_researcher", title: "Ведущий научный сотрудник: климатические модели",
			summary:      "Развитие региональной климатической модели Арктики, оценки изменений ледового покрова до 2050 года.",
			description:  "Отдел ведёт прогноз ледовой обстановки для судоходства и разрабатывает региональную модель климата. Нужен учёный, который возьмёт на себя направление и будет вести магистрантов.",
			requirements: "Доктор или кандидат наук с опытом численного моделирования атмосферы или океана, публикации в журналах первого и второго квартилей.",
			focus:        "Региональное климатическое моделирование", level: 3, format: "hybrid", region: arh, city: "Архангельск", housing: "service", rate: 100,
			salaryFrom: 140000, salaryTo: 190000, contract: "permanent", funding: "budget", degree: "candidate", daysLeft: 22,
			specialties: []string{"1.6.18", "1.6.17"}},
		{unit: 1, position: "intern_researcher", title: "Стажёр-исследователь: полевой сезон на мерзлоте",
			summary:     "Три месяца полевых измерений температуры грунтов и потоков метана на северных площадках.",
			description: "Стажировка для студентов старших курсов и выпускников. Проживание и питание в экспедиции за счёт центра, дорога оплачивается. По итогам лучших приглашаем в магистратуру и на штатные места.",
			focus:       "Полевые измерения", level: 1, format: "onsite", region: arh, city: "Архангельск", housing: "service", salaryFrom: 25000, contract: "fixed", months: 3,
			funding: "grant", daysLeft: 11, specialties: []string{"1.6.7", "1.6.8"}},
		{unit: -1, position: "grant_manager", title: "Менеджер научных проектов",
			summary:      "Сопровождение заявок на гранты, отчётность перед фондами, взаимодействие с партнёрами из Норвегии и Финляндии.",
			description:  "Вы будете готовить заявки в РНФ и другие фонды, следить за расходами проектов, собирать отчёты. Центр участвует в шести проектах одновременно.",
			requirements: "Опыт сопровождения научных проектов от двух лет, английский не ниже B2.",
			focus:        "Научные проекты и гранты", format: "onsite", region: arh, city: "Архангельск", rate: 100, salaryFrom: 80000, salaryTo: 110000, contract: "permanent", funding: "budget"},
		{unit: 0, position: "postdoc", title: "Постдок: морской лёд (набор закрыт)",
			summary:     "Позиция занята: постдок по дистанционному зондированию морского льда.",
			description: "Набор завершён в сентябре, позиция сохранена на странице организации для истории.",
			focus:       "Спутниковые данные о морском льде", level: 2, format: "hybrid", region: arh, city: "Архангельск", salaryFrom: 100000, contract: "fixed", months: 24, funding: "grant",
			specialties: []string{"1.6.19", "1.6.8"}, status: vacancies.StatusClosed},
	},
	"neurofotonika": {
		{unit: 0, position: "researcher", title: "Научный сотрудник: двухфотонная микроскопия",
			summary:      "Разработка и эксплуатация двухфотонных систем для записи активности нейронов у животных.",
			description:  "Команда из восьми человек: оптики, биологи и программисты. Вы будете настраивать лазерные системы, проводить эксперименты на мышах и участвовать в публикациях. Оборудование: два двухфотонных микроскопа, мини-микроскопы, собственная операционная.",
			requirements: "Кандидат наук или аспирант на последнем году по физике, оптике или нейробиологии; навыки работы с лазерами.",
			focus:        "Двухфотонная микроскопия in vivo", level: 2, format: "onsite", region: spb, city: "Санкт-Петербург", rate: 100, salaryFrom: 120000, salaryTo: 170000,
			contract: "permanent", funding: "own", degree: "candidate", daysLeft: 27, specialties: []string{"1.5.24", "1.3.6", "1.3.19"}},
		{unit: -1, position: "tech_transfer", title: "Специалист по трансферу технологий",
			summary:     "Поиск партнёров для коммерциализации оптических методов, подготовка патентных заявок, договоры с университетами.",
			description: "Компания хочет вывести два метода на рынок нейротехнологий. Вам предстоит оформить патенты, найти лицензиатов и вести переговоры.",
			focus:       "Патенты и лицензирование", format: "hybrid", region: spb, city: "Санкт-Петербург", rate: 100, salaryFrom: 100000, salaryTo: 140000, contract: "permanent", funding: "own"},
	},
	"tekhnopark-akadem-sever": {
		{unit: 0, position: "tech_transfer", title: "Руководитель центра трансфера технологий",
			summary:      "Руководство центром трансфера: патенты, лицензии, работа с университетами Томска.",
			description:  "Центр сопровождает около сорока разработок университетов. Нужен руководитель с управленческим опытом и пониманием, как работает патентование.",
			requirements: "Опыт управления группой от трёх человек, знание патентного права.",
			focus:        "Трансфер технологий", format: "onsite", region: tms, city: "Томск", rate: 100, salaryFrom: 120000, salaryTo: 160000, contract: "permanent",
			funding: "own", daysLeft: 50},
		{unit: 1, position: "research_engineer", title: "Инженер-исследователь по аддитивным технологиям",
			summary:     "Разработка режимов 3D-печати металлами и полимерами для стартапов технопарка.",
			description: "ЦКП «Прототипирование» помогает стартапам делать опытные образцы. Вы будете подбирать материалы и режимы печати, консультировать резидентов.",
			focus:       "Аддитивные технологии", level: 2, format: "onsite", region: tms, city: "Томск", rate: 100, salaryFrom: 80000, salaryTo: 110000, contract: "permanent",
			funding: "own", specialties: []string{"2.6.17", "2.6.5"}},
	},
	"yuzhnyy-institut-morskoy-biologii": {
		{unit: 0, position: "junior_researcher", title: "Младший научный сотрудник: фитопланктон",
			summary:      "Обработка проб и спутниковых данных о цветении воды в Чёрном море, участие в рейсах.",
			description:  "Лаборатория ведёт мониторинг цветения воды и участвует в двух рейсах в год. Для молодых специалистов предусмотрена программа подготовки диссертации.",
			requirements: "Высшее образование по биологии или экологии, желание работать в рейсах.",
			focus:        "Фитопланктон и спутниковый мониторинг", level: 1, format: "onsite", region: sev, city: "Севастополь", housing: "compensation", rate: 100,
			salaryFrom: 60000, salaryTo: 80000, contract: "fixed", months: 36, funding: "budget", daysLeft: 33, specialties: []string{"1.5.16", "1.5.15"}},
		{unit: 1, position: "senior_researcher", title: "Старший научный сотрудник: популяционная динамика рыб",
			summary:      "Оценка запасов промысловых рыб Азовского и Чёрного морей, разработка рекомендаций для рыбной отрасли.",
			description:  "Отдел готовит ежегодные прогнозы вылова. Нужен специалист по статистике популяций, который расширит модели и подготовит обзор для отрасли.",
			requirements: "Кандидат биологических наук, опыт анализа данных учётных съёмок.",
			focus:        "Популяционная динамика", level: 3, format: "hybrid", region: sev, city: "Севастополь", housing: "compensation", rate: 100, salaryFrom: 90000,
			salaryTo: 120000, contract: "permanent", funding: "budget", degree: "candidate", competition: true, daysLeft: 6, specialties: []string{"1.5.13", "1.5.15"}},
	},
	"dalnevostochnyy-tsentr-bioraznoobraziya": {
		{unit: 0, position: "intern_researcher", title: "Стажёр: оцифровка гербария",
			summary:     "Съёмка, описание и определение гербарных листов, работа с базой данных видов.",
			description: "Центр оцифровывает коллекцию в 400 тысяч листов. Стажёры учатся определять виды и вести каталог, лучшие продолжают работу над собственным исследованием.",
			focus:       "Гербарные коллекции", level: 1, format: "onsite", region: vvo, city: "Владивосток", rate: 50, salaryFrom: 25000, contract: "fixed", months: 6, funding: "grant",
			daysLeft: 20, specialties: []string{"1.5.9"}},
		{unit: -1, position: "phd_student", title: "Аспирантура: флора южного Приморья",
			summary:     "Тема исследования: изменение флоры горных лесов южного Приморья за последние сорок лет.",
			description: "Работа в гербарии и в поле, обработка данных с помощью геоинформационных систем. Жильё для иногородних в общежитии центра.",
			focus:       "Флора и растительность Приморья", level: 1, format: "onsite", region: vvo, city: "Владивосток", housing: "dormitory", salaryFrom: 35000, contract: "fixed",
			months: 48, funding: "budget", daysLeft: 52, specialties: []string{"1.5.9", "1.5.15"}},
	},
	"nizhegorodskiy-universitet-radiofiziki": {
		{unit: 0, position: "docent", title: "Доцент кафедры нелинейной динамики (конкурс)",
			summary:      "Чтение курсов по нелинейной динамике и теории колебаний, руководство магистрами, участие в проекте РНФ.",
			description:  "Кафедра занимается синхронизацией и математическими моделями нейронных сетей. Нагрузка 0,75 ставки, остальное — научная работа в проекте.",
			requirements: "Кандидат физико-математических наук, звание доцента приветствуется.",
			focus:        "Нелинейная динамика, теория колебаний", level: 3, format: "onsite", region: nnv, city: "Нижний Новгород", rate: 75, salaryFrom: 60000, salaryTo: 85000,
			contract: "fixed", months: 60, funding: "grant", note: "Грант РНФ, проект 23-72-00041", degree: "candidate", competition: true, daysLeft: 16,
			specialties: []string{"1.3.4", "1.1.2"}},
		{unit: 1, position: "researcher", title: "Научный сотрудник: терагерцовые детекторы",
			summary:      "Разработка и измерение полупроводниковых детекторов терагерцового излучения.",
			description:  "Лаборатория делает источники и детекторы для спектроскопии и неразрушающего контроля. Вы будете проектировать структуры, измерять характеристики, готовить заявки на гранты.",
			requirements: "Кандидат наук или магистр с опытом в физике полупроводников или радиофизике.",
			focus:        "Терагерцовые детекторы", level: 2, format: "onsite", region: nnv, city: "Нижний Новгород", rate: 100, salaryFrom: 85000, salaryTo: 115000,
			contract: "fixed", months: 36, funding: "grant", specialties: []string{"1.3.4", "1.3.11", "2.2.7"}},
		{unit: 1, position: "postdoc", title: "Постдок: терагерцовая фотоника",
			summary:     "Два года исследований терагерцовых волноводов и метаповерхностей.",
			description: "Вы будете вести один из проектов лаборатории, руководить двумя студентами и публиковаться в международных журналах.",
			focus:       "Терагерцовые метаповерхности", level: 2, format: "onsite", region: nnv, city: "Нижний Новгород", housing: "compensation", salaryFrom: 100000, salaryTo: 130000,
			contract: "fixed", months: 24, funding: "grant", degree: "candidate", daysLeft: 38, specialties: []string{"1.3.6", "2.2.7"}},
	},
}

// seedVacancies создаёт демонстрационные вакансии организации, если у неё их ещё нет. Возвращает число созданных.
func seedVacancies(ctx context.Context, tx pgx.Tx, q *dbgen.Queries, org dbgen.Organization, owner uuid.UUID, now time.Time) (int, error) {
	list := vacanciesByOrg[org.Slug]
	if len(list) == 0 {
		return 0, nil
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM vacancies WHERE org_id = $1`, org.ID).Scan(&existing); err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, nil
	}
	units, err := q.ListUnitNames(ctx, org.ID)
	if err != nil {
		return 0, err
	}
	created := 0
	for i, v := range list {
		var unit *uuid.UUID
		if v.unit >= 0 {
			// Подразделения создаются в порядке списка; ListUnitNames даёт порядок по названию, поэтому ищем по имени из организации.
			name := organizationUnitName(org.Slug, v.unit)
			for _, u := range units {
				if u.Name == name {
					id := u.ID
					unit = &id
				}
			}
			if unit == nil {
				return 0, fmt.Errorf("vacancy %q: unit %d not found", v.title, v.unit)
			}
		}
		params := dbgen.CreateVacancyParams{
			OrgID: org.ID, UnitID: unit, CreatedBy: &owner, Title: v.title, PositionCode: v.position, Summary: v.summary,
			Description: v.description, Requirements: v.requirements, Focus: v.focus, City: v.city, Housing: orDefault(v.housing, vacancies.HousingNone),
			FundingNote: v.note, DegreeRequired: orDefault(v.degree, vacancies.DegreeNone), TitleRequired: orDefault(v.academicTitle, vacancies.TitleNone),
			IsCompetition: v.competition,
			// Разное время создания, чтобы порядок «новые сверху» выглядел естественно.
			Now: now.Add(-time.Duration(len(list)-i) * 26 * time.Hour),
		}
		params.CareerLevel = int16Ptr(v.level)
		params.RatePercent = int16Ptr(v.rate)
		params.SalaryFrom = int32Ptr(v.salaryFrom)
		params.SalaryTo = int32Ptr(v.salaryTo)
		params.ContractMonths = int16Ptr(v.months)
		params.WorkFormat = strPtr(v.format)
		params.RegionCode = strPtr(v.region)
		params.ContractType = strPtr(v.contract)
		params.FundingSource = strPtr(v.funding)
		if v.daysLeft > 0 {
			d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, v.daysLeft)
			params.Deadline = &d
		}
		id, err := q.CreateVacancy(ctx, params)
		if err != nil {
			return 0, fmt.Errorf("vacancy %q: %w", v.title, err)
		}
		if len(v.specialties) > 0 {
			if err := q.AddVacancySpecialties(ctx, dbgen.AddVacancySpecialtiesParams{VacancyID: id, Codes: v.specialties}); err != nil {
				return 0, fmt.Errorf("vacancy %q: specialties: %w", v.title, err)
			}
		}
		status := orDefault(v.status, vacancies.StatusPublished)
		if status != vacancies.StatusDraft {
			steps := []string{vacancies.StatusPublished}
			if status == vacancies.StatusClosed {
				steps = append(steps, vacancies.StatusClosed)
			}
			from := vacancies.StatusDraft
			for _, to := range steps {
				if _, err := q.SetVacancyStatus(ctx, dbgen.SetVacancyStatusParams{ID: id, FromStatus: from, ToStatus: to, Now: params.Now.Add(time.Hour)}); err != nil {
					return 0, fmt.Errorf("vacancy %q: status: %w", v.title, err)
				}
				from = to
			}
		}
		created++
	}
	return created, nil
}

func organizationUnitName(slug string, index int) string {
	for _, o := range organizations {
		if o.slug == slug {
			return o.units[index].name
		}
	}
	return ""
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func int16Ptr(n int) *int16 {
	if n == 0 {
		return nil
	}
	v := int16(n)
	return &v
}

func int32Ptr(n int) *int32 {
	if n == 0 {
		return nil
	}
	v := int32(n)
	return &v
}
