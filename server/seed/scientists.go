package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/auth"
	prof "scibox/server/internal/profiles"
)

// Демо-учёные (срез 7): у каждого заполненный профиль. Режимы приватности разные, чтобы на странице профиля,
// в каталоге (срез 10) и в резюме было на чём проверить «скрыт / организациям / публичный».
// Публикации вымышлены: DOI взяты из префикса 10.5555 (Crossref использует его для тестов; случайные настоящие
// записи там тоже бывают, поэтому поиск этих DOI в Crossref ничего не гарантирует). ORCID взяты из диапазона
// 0000-0001-50xx-xxxx, который ORCID использует в песочнице (прямого подтверждения «людям не выдаётся» нет);
// номера Scopus, SPIN и WoS придуманы и могут случайно совпасть с настоящими. Контрольный знак ORCID считает orcid().

// extraPeople — люди без организаций: только профили учёных.
var extraPeople = []person{
	{"korolev", "dmitry.korolev@demo.example.ru", "Дмитрий Королёв"},
	{"lebedeva", "ksenia.lebedeva@demo.example.ru", "Ксения Лебедева"},
	{"morozov", "artem.morozov@demo.example.ru", "Артём Морозов"},
	{"zhukova", "natalia.zhukova@demo.example.ru", "Наталья Жукова"},
	{"shiryaev", "roman.shiryaev@demo.example.ru", "Роман Ширяев"},
	{"guseva", "alina.guseva@demo.example.ru", "Алина Гусева"},
	{"tarasov", "igor.tarasov@demo.example.ru", "Игорь Тарасов"},
	{"andreeva", "vera.andreeva@demo.example.ru", "Вера Андреева"},
}

// orcid строит ORCID iD вида 0000-0001-50NN-NNNC: n — пять цифр после «0000-0001-50», контрольный знак считается
// по ISO 7064 MOD 11-2.
func orcid(n int) string {
	first15 := fmt.Sprintf("0000000150%05d", n)
	total := 0
	for _, r := range first15 {
		total = (total + int(r-'0')) * 2
	}
	d := (12 - total%11) % 11
	check := string(rune('0' + d))
	if d == 10 {
		check = "X"
	}
	s := first15 + check
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func ip(n int) *int { return &n }

func edu(inst, program string, from, to int) prof.ItemInput {
	f := prof.ItemFields{Institution: inst, Program: program, YearFrom: ip(from)}
	if to > 0 {
		f.YearTo = ip(to)
	}
	return prof.ItemInput{Kind: prof.KindEducation, ItemFields: f}
}

func job(org, position, desc string, from, to int) prof.ItemInput {
	f := prof.ItemFields{Organization: org, Position: position, Description: desc, YearFrom: ip(from)}
	if to > 0 {
		f.YearTo = ip(to)
	}
	return prof.ItemInput{Kind: prof.KindExperience, ItemFields: f}
}

// journalISSN — настоящие ISSN журналов демо-публикаций из выгрузки SCImago 2025 (срез 14): квартиль берётся из справочника,
// а не придумывается. У российских журналов SCImago ранжирует переводную версию, поэтому ISSN — её. Журнала
// «Сверхпроводимость: исследования и разработки» в SCImago нет (одноимённый «Superconductivity» — другой журнал).
var journalISSN = map[string]string{
	"Физика твёрдого тела":                                     "1063-7834", // Physics of the Solid State
	"Журнал экспериментальной и теоретической физики":          "1063-7761", // Journal of Experimental and Theoretical Physics
	"Physical Review Materials":                                "2475-9953",
	"Успехи физических наук":                                   "1063-7869", // Physics-Uspekhi
	"Письма в журнал технической физики":                       "1063-7850", // Technical Physics Letters
	"Журнал вычислительной математики и математической физики": "0965-5425", // Computational Mathematics and Mathematical Physics
	"Дифференциальные уравнения":                               "0012-2661", // Differential Equations
	"Океанология":                                              "0001-4370", // Oceanology
	"Метеорология и гидрология":                                "1068-3739", // Russian Meteorology and Hydrology
	"Cold Regions Science and Technology":                      "0165-232X",
	"Журнал аналитической химии":                               "1061-9348", // Journal of Analytical Chemistry
	"Bioinformatics": "1367-4803",
	"Молекулярная биология":       "0026-8933", // Molecular Biology
	"Информатика и автоматизация": "2713-3192", // Informatics and Automation
	"Геохимия":                         "0016-7029", // Geochemistry International
	"Криосфера Земли":                  "1560-7496", // Earth's Cryosphere
	"Приборы и техника эксперимента":   "0020-4412", // Instruments and Experimental Techniques
	"Известия вузов. Математика":       "1066-369X", // Russian Mathematics
	"Микробиология":                    "0026-2617", // Microbiology (Russian Federation)
	"Extremophiles":                    "1431-0651",
	"Физика металлов и металловедение": "0031-918X", // Physics of Metals and Metallography
	"Перспективные материалы":          "2075-1133", // Inorganic Materials: Applied Research
	"Journal of Alloys and Compounds":  "0925-8388",
}

func pub(title, authors, venue string, year int, vol, issue, pages, doi string) prof.ItemInput {
	return prof.ItemInput{Kind: prof.KindPublication, ItemFields: prof.ItemFields{
		Title: title, Authors: authors, Venue: venue, PubType: "article", Year: ip(year), Volume: vol, Issue: issue, Pages: pages, DOI: doi,
		ISSN: journalISSN[venue],
	}}
}

func pubOf(kind, title, authors, venue string, year int) prof.ItemInput {
	return prof.ItemInput{Kind: prof.KindPublication, ItemFields: prof.ItemFields{
		Title: title, Authors: authors, Venue: venue, PubType: kind, Year: ip(year),
	}}
}

func grant(title, funder, number, role string, from, to int) prof.ItemInput {
	return prof.ItemInput{Kind: prof.KindGrant, ItemFields: prof.ItemFields{
		Title: title, Funder: funder, Number: number, Role: role, YearFrom: ip(from), YearTo: ip(to),
	}}
}

func patent(title, authors, number, office, kind string, year int) prof.ItemInput {
	return prof.ItemInput{Kind: prof.KindPatent, ItemFields: prof.ItemFields{
		Title: title, Authors: authors, Number: number, Office: office, PatentType: kind, Year: ip(year),
	}}
}

func teach(course, inst, level string, from, to int) prof.ItemInput {
	f := prof.ItemFields{Course: course, Institution: inst, Level: level, YearFrom: ip(from)}
	if to > 0 {
		f.YearTo = ip(to)
	}
	return prof.ItemInput{Kind: prof.KindTeaching, ItemFields: f}
}

type scientist struct {
	key          string
	visibility   string
	openToOffers bool
	core         prof.CoreInput
	items        []prof.ItemInput
}

var scientists = []scientist{
	{
		key: "orlova", visibility: "public", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Заместитель директора по науке, Сибирский институт квантовых материалов", City: "Новосибирск", RegionCode: "54",
			About:  "Занимаюсь сверхпроводящими плёнками и теорией электронной структуры. Руковожу отделом вычислительной физики, веду аспирантов. Ищу не работу, а сильных постдоков в команду.",
			Degree: "doctor", DegreeSpecialty: "1.3.8", DegreeYear: ip(2017), DegreeInstitution: "Институт физики полупроводников", Dissertation: "Электронная структура слоистых сверхпроводников",
			AcademicTitle: "professor", AcademicTitleYear: ip(2022),
			ORCID: orcid(101), SPIN: "3141-5926", ScopusID: "57200000101", WosID: "K-1101-2015",
			HRsci: ip(21), HScopus: ip(18), HWos: ip(17), HScholar: ip(24),
			ContactEmail: "elena.orlova@sikm.example.ru", Specialties: []string{"1.3.8", "1.3.3", "1.2.2"},
		},
		items: []prof.ItemInput{
			edu("Новосибирский государственный университет", "Физический факультет, специалитет", 2000, 2005),
			edu("Институт физики полупроводников", "Аспирантура, физика конденсированного состояния", 2005, 2008),
			job("Сибирский институт квантовых материалов", "Заместитель директора по науке", "Научная программа института, руководство отделом вычислительной физики.", 2021, 0),
			job("Сибирский институт квантовых материалов", "Ведущий научный сотрудник", "", 2015, 2021),
			job("Институт физики полупроводников", "Старший научный сотрудник", "", 2009, 2015),
			pub("Электронная структура слоистых сверхпроводников из первых принципов", "Орлова Е. А., Белов И. С.", "Физика твёрдого тела", 2024, "66", "3", "412–425", "10.5555/demo.2024.101"),
			pub("Влияние дефектов на критическую температуру тонких плёнок", "Орлова Е. А., Зайцева М. В., Морозов А. Д.", "Журнал экспериментальной и теоретической физики", 2022, "161", "6", "881–894", "10.5555/demo.2022.102"),
			pub("Machine-learned interatomic potentials for layered superconductors", "Orlova E. A., Fedorov D. K.", "Physical Review Materials", 2021, "5", "9", "094801", "10.5555/demo.2021.103"),
			pub("Топологические состояния на границе сверхпроводящих плёнок", "Орлова Е. А.", "Успехи физических наук", 2019, "189", "2", "133–158", "10.5555/demo.2019.104"),
			pubOf("book", "Вычислительные методы физики конденсированного состояния", "Орлова Е. А., Белов И. С.", "Новосибирск: Изд-во СО РАН", 2020),
			grant("Новые сверхпроводящие материалы со слоистой структурой", "РНФ", "24-12-00177", "lead", 2024, 2027),
			grant("Моделирование квантовых устройств на сверхпроводниках", "РНФ", "19-72-30010", "lead", 2019, 2023),
			patent("Способ определения критической температуры тонкой плёнки", "Орлова Е. А., Зайцева М. В.", "RU 2 745 123 C1", "Роспатент", "invention", 2021),
			teach("Физика конденсированного состояния", "Новосибирский государственный университет", "master", 2012, 0),
			teach("Методы вычислительной физики", "Новосибирский государственный университет", "postgraduate", 2016, 0),
		},
	},
	{
		key: "zaitseva", visibility: "orgs", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Заведующая лабораторией сверхпроводящих материалов", City: "Новосибирск", RegionCode: "54",
			About:  "Синтез и измерение плёнок высокотемпературных сверхпроводителей.",
			Degree: "candidate", DegreeSpecialty: "1.3.8", DegreeYear: ip(2012), DegreeInstitution: "Институт физики полупроводников", Dissertation: "Критический ток в тонких плёнках купратов",
			ORCID: orcid(102), SPIN: "2718-2818", HRsci: ip(14), HScopus: ip(12),
			ContactEmail: "marina.zaitseva@sikm.example.ru", Specialties: []string{"1.3.8", "1.4.4"},
		},
		items: []prof.ItemInput{
			edu("Новосибирский государственный университет", "Физический факультет", 2002, 2007),
			job("Сибирский институт квантовых материалов", "Заведующая лабораторией", "", 2019, 0),
			job("Сибирский институт квантовых материалов", "Старший научный сотрудник", "", 2012, 2019),
			pub("Критический ток плёнок YBCO на подложках с буферным слоем", "Зайцева М. В., Орлова Е. А.", "Сверхпроводимость: исследования и разработки", 2023, "12", "1", "34–47", "10.5555/demo.2023.201"),
			pub("Рост плёнок купратов методом магнетронного распыления", "Зайцева М. В.", "Письма в журнал технической физики", 2020, "46", "11", "9–12", "10.5555/demo.2020.202"),
			grant("Плёнки с рекордным критическим током", "РНФ", "21-79-10133", "lead", 2021, 2024),
		},
	},
	{
		key: "belov", visibility: "public", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Заведующий кафедрой прикладной математики", City: "Казань", RegionCode: "16",
			About:  "Численные методы, оптимизация, математическое моделирование для промышленности. Веду курсы для бакалавров и магистров.",
			Degree: "doctor", DegreeSpecialty: "1.1.6", DegreeYear: ip(2014), DegreeInstitution: "Казанский федеральный университет", Dissertation: "Методы оптимизации в задачах многофазной фильтрации",
			AcademicTitle: "professor", AcademicTitleYear: ip(2018),
			ORCID: orcid(103), SPIN: "1618-0339", ScopusID: "55600000303", HRsci: ip(17), HScopus: ip(13),
			Specialties: []string{"1.1.6", "1.1.2", "1.2.2"},
		},
		items: []prof.ItemInput{
			edu("Казанский федеральный университет", "Механико-математический факультет", 1996, 2001),
			job("Приволжский университет технологий", "Заведующий кафедрой прикладной математики", "", 2017, 0),
			job("Приволжский университет технологий", "Профессор", "", 2011, 2017),
			pub("Численное решение задач оптимизации многофазной фильтрации", "Белов И. С.", "Журнал вычислительной математики и математической физики", 2023, "63", "5", "802–817", "10.5555/demo.2023.301"),
			pub("Градиентные методы для задач с ограничениями на граф течения", "Белов И. С., Орлова Е. А.", "Дифференциальные уравнения", 2021, "57", "8", "1066–1079", "10.5555/demo.2021.302"),
			grant("Математические модели для промышленной оптимизации", "Минобрнауки России", "075-15-2022-1123", "participant", 2022, 2025),
			teach("Методы оптимизации", "Приволжский университет технологий", "master", 2005, 0),
			teach("Численные методы", "Приволжский университет технологий", "bachelor", 2003, 0),
		},
	},
	{
		key: "kuznetsova", visibility: "hidden", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Директор Центра изучения Арктики", City: "Архангельск", RegionCode: "29",
			Degree: "candidate", DegreeSpecialty: "1.6.17", DegreeYear: ip(2010), About: "Профиль скрыт: я нанимаю, а не ищу работу.", Specialties: []string{"1.6.17", "1.6.18"},
		},
		items: []prof.ItemInput{
			job("Центр изучения Арктики", "Директор", "", 2018, 0),
			pub("Ледовая обстановка в Белом море: многолетние изменения", "Кузнецова О. В.", "Океанология", 2019, "59", "4", "551–562", "10.5555/demo.2019.401"),
		},
	},
	{
		key: "fedorov", visibility: "orgs", openToOffers: true,
		core: prof.CoreInput{
			Headline: "Руководитель отдела климатического моделирования", City: "Архангельск", RegionCode: "29",
			About:  "Региональные модели климата, прогноз ледовой обстановки. Рассматриваю предложения от институтов, где есть вычислительные мощности и полевые данные.",
			Degree: "candidate", DegreeSpecialty: "1.6.18", DegreeYear: ip(2015), DegreeInstitution: "Институт мониторинга климатических и экологических систем",
			ORCID: orcid(104), SPIN: "5772-1566", HRsci: ip(9), HScopus: ip(8), HScholar: ip(11),
			ContactEmail: "denis.fedorov@arctic-center.example.ru", Specialties: []string{"1.6.18", "1.6.17", "1.6.8"},
		},
		items: []prof.ItemInput{
			edu("Московский государственный университет", "Географический факультет", 2003, 2008),
			job("Центр изучения Арктики", "Руководитель отдела климатического моделирования", "", 2019, 0),
			job("Институт мониторинга климатических и экологических систем", "Научный сотрудник", "", 2012, 2019),
			pub("Региональная модель климата для Баренцева региона: оценка на 2040 год", "Фёдоров Д. К., Кузнецова О. В.", "Метеорология и гидрология", 2024, "", "2", "5–19", "10.5555/demo.2024.501"),
			pub("Sea-ice forecast skill of a coupled regional model", "Fedorov D. K.", "Cold Regions Science and Technology", 2022, "198", "", "103540", "10.5555/demo.2022.502"),
			pubOf("conference", "Данные дистанционного зондирования для прогноза ледовой обстановки", "Фёдоров Д. К., Андреева В. Н.", "Материалы конференции «Арктика-2023»", 2023),
			grant("Прогноз ледовой обстановки на сезон", "РНФ", "23-27-00188", "lead", 2023, 2026),
		},
	},
	{
		key: "vasilev", visibility: "public", openToOffers: true,
		core: prof.CoreInput{
			Headline: "Кадровик, бывший химик-аналитик", City: "Новосибирск", RegionCode: "54",
			About:  "Помогаю научным организациям с набором. Раньше работал в аналитической лаборатории.",
			Degree: "candidate", DegreeSpecialty: "1.4.2", DegreeYear: ip(2013), Specialties: []string{"1.4.2"},
		},
		items: []prof.ItemInput{
			job("Институт аналитической химии", "Научный сотрудник", "", 2013, 2020),
			pub("Определение следов тяжёлых металлов методом инверсионной вольтамперометрии", "Васильев А. П.", "Журнал аналитической химии", 2018, "73", "9", "690–698", "10.5555/demo.2018.601"),
		},
	},
	{
		key: "korolev", visibility: "public", openToOffers: true,
		core: prof.CoreInput{
			Headline: "Научный сотрудник, лаборатория биоинформатики", City: "Казань", RegionCode: "16",
			About:  "Анализ геномов и транскриптомов клинических образцов. Пишу пайплайны на Nextflow и Python, читаю лекции по статистике для биологов. Открыт к предложениям из лабораторий, где работают с секвенированием.",
			Degree: "candidate", DegreeSpecialty: "1.5.8", DegreeYear: ip(2020), DegreeInstitution: "Казанский федеральный университет", Dissertation: "Методы обнаружения структурных вариаций по данным секвенирования",
			ORCID: orcid(105), SPIN: "7821-4410", ScopusID: "57210000111", WosID: "AAB-4411-2020", HRsci: ip(6), HScopus: ip(7), HWos: ip(5), HScholar: ip(9),
			ContactEmail: "dmitry.korolev@example.ru", Specialties: []string{"1.5.8", "1.5.7"},
		},
		items: []prof.ItemInput{
			edu("Казанский федеральный университет", "Институт фундаментальной медицины и биологии, магистратура", 2013, 2015),
			edu("Казанский федеральный университет", "Аспирантура, математическая биология и биоинформатика", 2016, 2020),
			job("Приволжский университет технологий", "Научный сотрудник, лаборатория биоинформатики", "Анализ данных секвенирования, поддержка пайплайнов для клинических групп.", 2020, 0),
			pub("Detecting structural variants in low-coverage long-read data", "Korolev D. A., Belov I. S., Kuznetsova O. V.", "Bioinformatics", 2023, "39", "7", "btad412", "10.5555/demo.2023.1101"),
			pub("Сравнение методов вызова вариантов для клинических экзомов", "Королёв Д. А.", "Молекулярная биология", 2022, "56", "3", "401–410", "10.5555/demo.2022.1102"),
			pub("Воспроизводимые пайплайны анализа транскриптомов: опыт лаборатории", "Королёв Д. А., Миронова Т. С., Белов И. С.", "Информатика и автоматизация", 2021, "20", "5", "1120–1145", "10.5555/demo.2021.1103"),
			pubOf("preprint", "Benchmarking annotation tools for non-model genomes", "Korolev D. A.", "bioRxiv", 2024),
			grant("Методы анализа структурных вариаций генома", "РНФ", "20-74-00055", "lead", 2020, 2023),
			teach("Биостатистика для биологов", "Приволжский университет технологий", "master", 2021, 0),
		},
	},
	{
		key: "lebedeva", visibility: "orgs", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Старший научный сотрудник, геохимия", City: "Томск", RegionCode: "70",
			About:  "Геохимия редких элементов в мерзлотных почвах.",
			Degree: "candidate", DegreeSpecialty: "1.6.4", DegreeYear: ip(2016), DegreeInstitution: "Томский государственный университет",
			ORCID: orcid(106), SPIN: "4415-9087", ScopusID: "56800000112", HRsci: ip(8), HScopus: ip(6),
			ContactEmail: "ksenia.lebedeva@example.ru", Specialties: []string{"1.6.4", "1.6.7"},
		},
		items: []prof.ItemInput{
			job("Томский государственный университет", "Старший научный сотрудник", "", 2018, 0),
			pub("Редкоземельные элементы в многолетнемёрзлых грунтах Западной Сибири", "Лебедева К. И., Андреева В. Н.", "Геохимия", 2022, "67", "11", "1042–1057", "10.5555/demo.2022.1201"),
			pub("Миграция лантаноидов в сезонно-талом слое", "Лебедева К. И.", "Криосфера Земли", 2020, "24", "4", "62–71", "10.5555/demo.2020.1202"),
			grant("Редкие элементы в мерзлотных почвах", "РНФ", "22-27-00402", "participant", 2022, 2025),
		},
	},
	{
		key: "morozov", visibility: "public", openToOffers: true,
		core: prof.CoreInput{
			Headline: "Аспирант, физика конденсированного состояния", City: "Новосибирск", RegionCode: "54",
			About:  "Третий год аспирантуры, измеряю тонкие плёнки при низких температурах. Ищу постдок через год.",
			Degree: "none", Specialties: []string{"1.3.8"}, ORCID: orcid(107), HScopus: ip(2),
		},
		items: []prof.ItemInput{
			edu("Новосибирский государственный университет", "Магистратура, физика", 2019, 2021),
			edu("Сибирский институт квантовых материалов", "Аспирантура, физика конденсированного состояния", 2023, 0),
			pub("Измерение критического тока плёнок при температурах ниже 4 К", "Морозов А. Д., Зайцева М. В.", "Приборы и техника эксперимента", 2024, "", "1", "102–108", "10.5555/demo.2024.1301"),
		},
	},
	{
		key: "zhukova", visibility: "public", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Доцент кафедры математического анализа", City: "Екатеринбург", RegionCode: "66",
			About:  "Дифференциальные уравнения и математическая физика. Преподаю анализ и уравнения математической физики, веду кружок для школьников.",
			Degree: "doctor", DegreeSpecialty: "1.1.2", DegreeYear: ip(2019), DegreeInstitution: "Уральский федеральный университет", Dissertation: "Краевые задачи для вырождающихся эллиптических уравнений",
			AcademicTitle: "docent", AcademicTitleYear: ip(2015),
			ORCID: orcid(108), SPIN: "6603-2217", ScopusID: "55400000114", HRsci: ip(11), HScopus: ip(7),
			ContactEmail: "natalia.zhukova@example.ru", Specialties: []string{"1.1.2", "1.1.1"},
		},
		items: []prof.ItemInput{
			edu("Уральский федеральный университет", "Математико-механический факультет", 1998, 2003),
			job("Уральский федеральный университет", "Доцент кафедры математического анализа", "", 2014, 0),
			pub("Краевые задачи для вырождающихся эллиптических уравнений с нелокальным условием", "Жукова Н. В.", "Известия вузов. Математика", 2023, "", "6", "34–49", "10.5555/demo.2023.1401"),
			pub("О разрешимости одной задачи для уравнения смешанного типа", "Жукова Н. В., Орлов П. С.", "Дифференциальные уравнения", 2020, "56", "9", "1190–1203", "10.5555/demo.2020.1402"),
			pubOf("book", "Уравнения математической физики: сборник задач", "Жукова Н. В.", "Екатеринбург: Изд-во Урал. ун-та", 2021),
			teach("Математический анализ", "Уральский федеральный университет", "bachelor", 2004, 0),
			teach("Уравнения математической физики", "Уральский федеральный университет", "bachelor", 2010, 0),
			teach("Спецкурс «Нелокальные задачи»", "Уральский федеральный университет", "master", 2019, 0),
		},
	},
	{
		key: "shiryaev", visibility: "hidden", openToOffers: true,
		core: prof.CoreInput{
			Headline: "Инженер-исследователь, оптика", City: "Санкт-Петербург", RegionCode: "78",
			Degree: "none", Specialties: []string{"1.3.6"}, About: "Профиль скрыт: пока присматриваюсь.",
		},
		items: []prof.ItemInput{
			job("Университет оптических систем", "Инженер-исследователь", "", 2021, 0),
		},
	},
	{
		key: "guseva", visibility: "orgs", openToOffers: true,
		core: prof.CoreInput{
			Headline: "Научный сотрудник, микробиология", City: "Москва", RegionCode: "77",
			About:  "Микробные сообщества почв и водоёмов, метагеномика. Ищу позицию, где можно совмещать полевые выезды и лабораторную работу.",
			Degree: "candidate", DegreeSpecialty: "1.5.11", DegreeYear: ip(2021), DegreeInstitution: "Московский государственный университет", Dissertation: "Структура микробных сообществ термальных источников",
			ORCID: orcid(109), SPIN: "8123-9004", ScopusID: "57220000116", WosID: "ABC-9116-2021", HRsci: ip(5), HScopus: ip(6), HWos: ip(4),
			ContactEmail: "alina.guseva@example.ru", Specialties: []string{"1.5.11", "1.5.15", "1.5.8"},
		},
		items: []prof.ItemInput{
			edu("Московский государственный университет", "Биологический факультет", 2011, 2016),
			edu("Московский государственный университет", "Аспирантура, микробиология", 2016, 2020),
			job("Институт микробиологии", "Научный сотрудник", "", 2021, 0),
			pub("Структура микробных сообществ термальных источников Камчатки", "Гусева А. Р., Лебедева К. И.", "Микробиология", 2022, "91", "4", "455–468", "10.5555/demo.2022.1601"),
			pub("Metagenomic survey of alkaline hot springs", "Guseva A. R.", "Extremophiles", 2024, "28", "1", "9", "10.5555/demo.2024.1602"),
			grant("Микробные сообщества горячих источников", "РНФ", "21-74-00031", "lead", 2021, 2024),
		},
	},
	{
		key: "tarasov", visibility: "public", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Профессор, главный научный сотрудник, материаловедение", City: "Томск", RegionCode: "70",
			About:  "Порошковая металлургия и композиционные материалы. Создал научную школу по спечённым композитам, подготовил четырнадцать кандидатов наук.",
			Degree: "doctor", DegreeSpecialty: "2.6.5", DegreeYear: ip(2005), DegreeInstitution: "Томский политехнический университет", Dissertation: "Закономерности структурообразования в спечённых композитах",
			AcademicTitle: "professor", AcademicTitleYear: ip(2008),
			ORCID: orcid(110), SPIN: "1056-3321", ScopusID: "7004000117", WosID: "B-2117-2009", HRsci: ip(29), HScopus: ip(22), HWos: ip(20), HScholar: ip(27),
			ContactEmail: "igor.tarasov@example.ru", Specialties: []string{"2.6.5", "2.6.1", "1.3.8"},
		},
		items: []prof.ItemInput{
			edu("Томский политехнический университет", "Физико-технический факультет", 1980, 1985),
			job("Томский политехнический университет", "Главный научный сотрудник", "", 2016, 0),
			job("Томский политехнический университет", "Заведующий кафедрой материаловедения", "", 2006, 2016),
			pub("Структурообразование в спечённых композитах на основе карбида титана при добавлении нанопорошков, стабилизированных в условиях высокоскоростного нагрева", "Тарасов И. В., Андреев С. П., Громов А. А., Лебедев В. Н., Морозова Е. Д.", "Физика металлов и металловедение", 2023, "124", "8", "715–731", "10.5555/demo.2023.1701"),
			pub("Спечённые композиты: от порошка к изделию", "Тарасов И. В.", "Перспективные материалы", 2021, "", "7", "5–22", "10.5555/demo.2021.1702"),
			pub("Mechanical properties of nanoreinforced titanium carbide composites", "Tarasov I. V., Gromov A. A.", "Journal of Alloys and Compounds", 2019, "810", "", "151840", "10.5555/demo.2019.1703"),
			pubOf("book", "Порошковая металлургия: учебное пособие", "Тарасов И. В., Громов А. А.", "Томск: Изд-во ТПУ", 2018),
			patent("Способ получения спечённого композита на основе карбида титана", "Тарасов И. В., Громов А. А.", "RU 2 701 445 C1", "Роспатент", "invention", 2019),
			patent("Программа расчёта режимов спекания композитов", "Тарасов И. В.", "RU 2023612345", "Роспатент", "software", 2023),
			grant("Нанопорошки в спечённых композитах", "РНФ", "18-19-00250", "lead", 2018, 2021),
			grant("Композиты для высоких температур", "Минобрнауки России", "FSWW-2024-0007", "lead", 2024, 2026),
			teach("Материаловедение", "Томский политехнический университет", "bachelor", 1990, 0),
			teach("Порошковая металлургия", "Томский политехнический университет", "master", 2000, 0),
			teach("Научная работа аспиранта", "Томский политехнический университет", "postgraduate", 2008, 0),
		},
	},
	{
		key: "andreeva", visibility: "hidden", openToOffers: false,
		core: prof.CoreInput{
			Headline: "Младший научный сотрудник, экология Арктики", City: "Архангельск", RegionCode: "29",
			Degree: "none", Specialties: []string{"1.5.15"}, About: "Профиль скрыт, пока нет публикаций.",
		},
		items: []prof.ItemInput{
			job("Центр изучения Арктики", "Младший научный сотрудник", "", 2023, 0),
		},
	},
}

// seedScientists заполняет профили демо-учёных через сервис профилей: так каждое поле проходит те же проверки,
// что и в приложении. Профиль, в котором уже есть содержимое, не трогаем (повторный запуск ничего не дублирует).
// newProfiles — сервис профилей для демо-данных (Crossref не нужен: все публикации вводятся вручную).
func newProfiles(pool *pgxpool.Pool) *prof.Service {
	return prof.NewService(pool, nil, prof.DefaultConfig("SciBox"))
}

func seedScientists(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID) (int, error) {
	svc := newProfiles(pool)
	created := 0
	for _, s := range scientists {
		user := auth.User{ID: ids[s.key]}
		page, err := svc.Own(ctx, user)
		if err != nil {
			return 0, fmt.Errorf("seed profile %s: %w", s.key, err)
		}
		if page.Profile.Headline != "" {
			continue
		}
		if _, err := svc.SaveCore(ctx, user, s.core); err != nil {
			return 0, fmt.Errorf("seed profile %s: %w", s.key, err)
		}
		if _, err := svc.SetPrivacy(ctx, user, s.visibility, s.openToOffers); err != nil {
			return 0, fmt.Errorf("seed profile %s: %w", s.key, err)
		}
		for _, it := range s.items {
			if _, err := svc.AddItem(ctx, user, it); err != nil {
				return 0, fmt.Errorf("seed profile %s (%s %q): %w", s.key, it.Kind, it.Title+it.Course+it.Position, err)
			}
		}
		created++
	}
	return created, nil
}
