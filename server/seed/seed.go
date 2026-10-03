// Package seed загружает демо-данные: вымышленные организации и люди, реальные города (D-022).
// Команда: make seed (или make db-reset: пересоздать базу и загрузить демо-данные).
// Код не входит в измеряемое покрытие (docs/TESTING.md): это служебный скрипт; его запуск проверяет тест в internal/cli.
package seed

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

// DemoPassword — пароль всех демонстрационных аккаунтов. Только для локальной разработки.
const DemoPassword = "demo-password-2026"

// Result — сколько записей создано (повторный запуск ничего не дублирует).
type Result struct {
	People        int
	Organizations int
	Vacancies     int
}

type person struct{ key, email, name string }

type memberSeed struct {
	who  string
	role access.Role
}

type unitSeed struct {
	name, kind, description string
	topics                  []string
	head                    string // ключ человека; пусто, если руководителя нет
}

type orgSeed struct {
	slug, name, kind, city, website, description string
	members                                      []memberSeed
	units                                        []unitSeed
	// invite — человек, которого приглашают в организацию (приглашение ждёт его в «Организации»).
	invite *memberSeed
}

var people = []person{
	{"orlova", "elena.orlova@demo.example.ru", "Елена Орлова"},
	{"sokolov", "pavel.sokolov@demo.example.ru", "Павел Соколов"},
	{"zaitseva", "marina.zaitseva@demo.example.ru", "Марина Зайцева"},
	{"belov", "ilya.belov@demo.example.ru", "Илья Белов"},
	{"kuznetsova", "olga.kuznetsova@demo.example.ru", "Ольга Кузнецова"},
	{"fedorov", "denis.fedorov@demo.example.ru", "Денис Фёдоров"},
	{"nikitin", "sergey.nikitin@demo.example.ru", "Сергей Никитин"},
	{"gromova", "anna.gromova@demo.example.ru", "Анна Громова"},
	{"vasilev", "anton.vasilev@demo.example.ru", "Антон Васильев"}, // без организации: ему пришло приглашение
}

var organizations = []orgSeed{
	{
		slug: "sibirskiy-institut-kvantovykh-materialov", name: "Сибирский институт квантовых материалов", kind: orgs.KindInstitute,
		city: "Новосибирск", website: "https://sikm.example.ru",
		description: "Институт изучает сверхпроводники, топологические материалы и квантовые устройства. Работаем в Академгородке с 1991 года, принимаем аспирантов и постдоков.",
		members:     []memberSeed{{"orlova", access.RoleOwner}, {"sokolov", access.RoleHR}, {"zaitseva", access.RoleUnitHead}},
		units: []unitSeed{
			{"Лаборатория сверхпроводящих материалов", orgs.UnitLaboratory, "Синтез и измерение плёнок и кристаллов с высокой критической температурой.", []string{"Сверхпроводимость", "Тонкие плёнки", "Низкие температуры"}, "zaitseva"},
			{"Отдел вычислительной физики", orgs.UnitDivision, "Расчёты электронной структуры и моделирование квантовых систем на кластере института.", []string{"Теория функционала плотности", "Машинное обучение в физике"}, "orlova"},
			{"ЦКП «Электронная микроскопия»", orgs.UnitSharedFacility, "Просвечивающие и сканирующие микроскопы для научных групп города.", []string{"Электронная микроскопия", "Подготовка образцов"}, ""},
		},
		invite: &memberSeed{"vasilev", access.RoleHR},
	},
	{
		slug: "privolzhskiy-universitet-tekhnologiy", name: "Приволжский университет технологий", kind: orgs.KindUniversity,
		city: "Казань", website: "https://put.example.ru",
		description: "Технический университет: химия, прикладная математика, биоинформатика. Набор на кафедры ведётся круглый год, конкурсы на должности ППС проходят весной и осенью.",
		members:     []memberSeed{{"belov", access.RoleOwner}, {"kuznetsova", access.RoleHR}},
		units: []unitSeed{
			{"Кафедра органической химии", orgs.UnitDepartment, "Синтез лекарственных кандидатов, зелёная химия, учебные курсы для бакалавров и магистров.", []string{"Органический синтез", "Катализ", "Зелёная химия"}, ""},
			{"Кафедра прикладной математики", orgs.UnitDepartment, "Численные методы, оптимизация, математическое моделирование для промышленности.", []string{"Оптимизация", "Дифференциальные уравнения"}, "belov"},
			{"Лаборатория биоинформатики", orgs.UnitLaboratory, "Анализ геномов и транскриптомов, методы для клинических данных.", []string{"Геномика", "Анализ секвенирования", "Статистика"}, ""},
		},
	},
	{
		slug: "tsentr-izucheniya-arktiki", name: "Центр изучения Арктики", kind: orgs.KindScienceCenter,
		city: "Архангельск", website: "https://arctic-center.example.ru",
		description: "Экспедиции, мониторинг морских льдов и климатические модели для северных регионов. Часть сотрудников работает в полевых сезонах.",
		members:     []memberSeed{{"kuznetsova", access.RoleOwner}, {"fedorov", access.RoleUnitHead}},
		units: []unitSeed{
			{"Отдел климатического моделирования", orgs.UnitDivision, "Региональные модели климата и прогнозы ледовой обстановки.", []string{"Климатические модели", "Морской лёд"}, "fedorov"},
			{"Лаборатория вечной мерзлоты", orgs.UnitLaboratory, "Температурный режим грунтов, выбросы метана, полевые наблюдения.", []string{"Мерзлота", "Метан", "Полевые измерения"}, ""},
		},
	},
	{
		slug: "neurofotonika", name: "Нейрофотоника", kind: orgs.KindRDCompany,
		city: "Санкт-Петербург", website: "https://neurophotonics.example.ru",
		description: "Компания с собственной лабораторией: оптические методы записи активности мозга и программное обеспечение для их анализа.",
		members:     []memberSeed{{"nikitin", access.RoleOwner}},
		units: []unitSeed{
			{"Исследовательская группа нейроимиджинга", orgs.UnitLaboratory, "Двухфотонная микроскопия и обработка изображений in vivo.", []string{"Двухфотонная микроскопия", "Обработка изображений"}, "nikitin"},
		},
	},
	{
		slug: "tekhnopark-akadem-sever", name: "Технопарк «Академ-Север»", kind: orgs.KindTechnopark,
		city: "Томск", website: "https://akadem-sever.example.ru",
		description: "Инновационная инфраструктура рядом с университетами: акселератор, центр трансфера технологий, лаборатории для стартапов.",
		members:     []memberSeed{{"gromova", access.RoleOwner}, {"sokolov", access.RoleHR}},
		units: []unitSeed{
			{"Центр трансфера технологий", orgs.UnitDivision, "Патентование, лицензирование и сопровождение разработок из университетов.", []string{"Трансфер технологий", "Интеллектуальная собственность"}, ""},
			{"ЦКП «Прототипирование»", orgs.UnitSharedFacility, "3D-печать, станки с ЧПУ и электроника для опытных образцов.", []string{"Аддитивные технологии", "Электроника"}, ""},
		},
	},
	{
		slug: "yuzhnyy-institut-morskoy-biologii", name: "Южный институт морской биологии", kind: orgs.KindInstitute,
		city: "Севастополь", website: "https://marine-bio.example.ru",
		description: "Исследуем биоразнообразие Чёрного и Азовского морей, рыбные ресурсы и последствия потепления для прибрежных экосистем.",
		members:     []memberSeed{{"fedorov", access.RoleOwner}, {"zaitseva", access.RoleHR}},
		units: []unitSeed{
			{"Лаборатория экологии планктона", orgs.UnitLaboratory, "Динамика фитопланктона и цветения воды, спутниковые наблюдения.", []string{"Планктон", "Дистанционное зондирование"}, ""},
			{"Отдел ихтиологии", orgs.UnitDivision, "Популяции промысловых рыб, мечение и учёты.", []string{"Ихтиология", "Популяционная динамика"}, ""},
		},
	},
	{
		slug: "dalnevostochnyy-tsentr-bioraznoobraziya", name: "Дальневосточный центр биоразнообразия", kind: orgs.KindScienceCenter,
		city: "Владивосток", website: "https://dvbio.example.ru",
		description: "Гербарии, коллекции насекомых и база данных видов Дальнего Востока. Принимаем стажёров и магистрантов на полевые сезоны.",
		members:     []memberSeed{{"gromova", access.RoleOwner}},
		units: []unitSeed{
			{"Гербарий и коллекции", orgs.UnitDivision, "Оцифровка и определение коллекционных образцов.", []string{"Ботаника", "Оцифровка коллекций"}, ""},
		},
	},
	{
		slug: "nizhegorodskiy-universitet-radiofiziki", name: "Нижегородский университет радиофизики", kind: orgs.KindUniversity,
		city: "Нижний Новгород", website: "https://radiophys.example.ru",
		description: "Радиофизика, фотоника и нелинейная динамика. Кафедры участвуют в проектах РНФ и работают с промышленными партнёрами.",
		members:     []memberSeed{{"nikitin", access.RoleOwner}, {"belov", access.RoleHR}},
		units: []unitSeed{
			{"Кафедра нелинейной динамики", orgs.UnitDepartment, "Хаос, синхронизация, математические модели нейронных сетей.", []string{"Нелинейная динамика", "Синхронизация"}, ""},
			{"Лаборатория терагерцовой фотоники", orgs.UnitLaboratory, "Источники и детекторы терагерцового излучения.", []string{"Терагерцовое излучение", "Фотоника"}, ""},
		},
	},
}

// Run загружает демо-данные в одной транзакции. Уже загруженное (по почте и адресу организации) не трогает.
func Run(ctx context.Context, pool *pgxpool.Pool, now time.Time) (Result, error) {
	hasher := auth.NewHasher(auth.DefaultHashParams, 1)
	hash, err := hasher.Hash(ctx, DemoPassword)
	if err != nil {
		return Result{}, fmt.Errorf("seed: hash password: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("seed: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)

	var res Result
	ids := map[string]uuid.UUID{}
	for _, p := range people {
		user, err := q.CreateUser(ctx, dbgen.CreateUserParams{
			Email: p.email, DisplayName: p.name, PasswordHash: hash, PrivacyConsentAt: now, PrivacyPolicyVersion: auth.PolicyVersion,
		})
		switch {
		case err == nil:
			res.People++
		case errors.Is(err, pgx.ErrNoRows):
			if user, err = q.GetUserByEmail(ctx, p.email); err != nil {
				return Result{}, fmt.Errorf("seed: load %s: %w", p.email, err)
			}
		default:
			return Result{}, fmt.Errorf("seed: create %s: %w", p.email, err)
		}
		if err := q.ConfirmUserEmail(ctx, dbgen.ConfirmUserEmailParams{At: now, ID: user.ID}); err != nil {
			return Result{}, fmt.Errorf("seed: confirm %s: %w", p.email, err)
		}
		ids[p.key] = user.ID
	}

	for _, o := range organizations {
		created, err := seedOrganization(ctx, q, o, ids, now)
		if err != nil {
			return Result{}, fmt.Errorf("seed: %s: %w", o.slug, err)
		}
		if created {
			res.Organizations++
		}
		org, err := q.GetOrganizationBySlug(ctx, o.slug)
		if err != nil {
			return Result{}, fmt.Errorf("seed: %s: %w", o.slug, err)
		}
		n, err := seedVacancies(ctx, tx, q, org, ids[o.members[0].who], now)
		if err != nil {
			return Result{}, fmt.Errorf("seed: %s: %w", o.slug, err)
		}
		res.Vacancies += n
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("seed: commit: %w", err)
	}
	return res, nil
}

func seedOrganization(ctx context.Context, q *dbgen.Queries, o orgSeed, ids map[string]uuid.UUID, now time.Time) (bool, error) {
	owner := ids[o.members[0].who]
	org, err := q.CreateOrganization(ctx, dbgen.CreateOrganizationParams{
		Slug: o.slug, Name: o.name, Kind: o.kind, City: o.city, Website: o.website, Description: o.description, CreatedBy: &owner, CreatedAt: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // уже загружена
	}
	if err != nil {
		return false, err
	}
	for _, m := range o.members {
		if err := q.AddMember(ctx, dbgen.AddMemberParams{OrgID: org.ID, UserID: ids[m.who], Role: string(m.role), JoinedAt: now}); err != nil {
			return false, err
		}
	}
	var firstUnit *uuid.UUID
	for _, u := range o.units {
		unit, err := q.CreateUnit(ctx, dbgen.CreateUnitParams{OrgID: org.ID, Name: u.name, Kind: u.kind, Description: u.description, Topics: u.topics, CreatedAt: now})
		if err != nil {
			return false, err
		}
		if firstUnit == nil {
			firstUnit = &unit.ID
		}
		if u.head != "" {
			head := ids[u.head]
			if _, err := q.SetUnitHead(ctx, dbgen.SetUnitHeadParams{ID: unit.ID, OrgID: org.ID, HeadUserID: &head, UpdatedAt: now}); err != nil {
				return false, err
			}
		}
	}
	if o.invite != nil {
		// Ссылки у демонстрационного приглашения нет (токен случайный и никому не показан): его принимают из списка в «Организации».
		sum := sha256.Sum256([]byte(uuid.NewString()))
		email := ""
		for _, p := range people {
			if p.key == o.invite.who {
				email = p.email
			}
		}
		if _, err := q.CreateInvitation(ctx, dbgen.CreateInvitationParams{
			OrgID: org.ID, Email: email, Role: string(o.invite.role), TokenHash: sum[:], InvitedBy: &owner, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Logins — почты демонстрационных аккаунтов (пароль DemoPassword).
func Logins() []string {
	out := make([]string, 0, len(people))
	for _, p := range people {
		out = append(out, p.email)
	}
	return out
}
