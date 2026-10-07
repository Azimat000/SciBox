// Package vacancies — вакансии: черновик, публикация, закрытие, архив, списки и страницы (срез 5).
//
// Критичная зона (docs/TESTING.md): здесь решается, кто и какую вакансию видит и меняет. Права решает пакет access
// (ManageVacancies); этот пакет собирает сведения о человеке через orgs.ActorOf и спрашивает access.
package vacancies

import (
	"time"

	"github.com/google/uuid"
)

// Статусы вакансии (жизненный цикл). Строки лежат в базе.
const (
	StatusDraft     = "draft"     // черновик: видят только те, кто ведёт вакансии
	StatusPublished = "published" // опубликована: видна всем, принимает отклики
	StatusClosed    = "closed"    // закрыта: набор закончен, страница остаётся по ссылке
	StatusArchived  = "archived"  // в архиве: убрана из списков, видят только те, кто ведёт вакансии
)

// Statuses — все статусы в порядке жизненного цикла.
var Statuses = []string{StatusDraft, StatusPublished, StatusClosed, StatusArchived}

// Виды вакансий (D-006, D-134). Вид определяет должность из справочника positions.
const (
	TypeResearch   = "research"   // научный работник (и постдок)
	TypeTeaching   = "teaching"   // преподаватель (ППС)
	TypeAdmin      = "admin"      // административный сотрудник: методисты, отделы, грант-менеджеры, ЦКП, руководство
	TypePhD        = "phd"        // аспирантура
	TypeMasters    = "masters"    // магистратура
	TypeProject    = "project"    // проектная работа: исполнители по грантам, специалисты проектов
	TypeInternship = "internship" // стажировка
)

// PositionTypes — все виды вакансий в порядке показа.
var PositionTypes = []string{TypeResearch, TypeTeaching, TypeAdmin, TypePhD, TypeMasters, TypeProject, TypeInternship}

// Формат работы.
const (
	FormatOnsite = "onsite" // очно
	FormatHybrid = "hybrid" // гибрид
	FormatRemote = "remote" // удалённо
)

// WorkFormats — все форматы работы.
var WorkFormats = []string{FormatOnsite, FormatHybrid, FormatRemote}

// Жильё.
const (
	HousingNone         = "none"         // не предоставляется
	HousingDormitory    = "dormitory"    // общежитие
	HousingService      = "service"      // служебное жильё
	HousingCompensation = "compensation" // компенсация аренды
)

// Housings — все варианты жилья.
var Housings = []string{HousingNone, HousingDormitory, HousingService, HousingCompensation}

// Вид договора.
const (
	ContractPermanent = "permanent" // бессрочный
	ContractFixed     = "fixed"     // срочный
)

// ContractTypes — все виды договора.
var ContractTypes = []string{ContractPermanent, ContractFixed}

// Источник финансирования (DOMAIN: бюджет, грант, хоздоговор, внебюджет).
const (
	FundingBudget   = "budget"   // бюджет
	FundingGrant    = "grant"    // грант
	FundingContract = "contract" // хоздоговор
	FundingOwn      = "own"      // внебюджетные средства
)

// FundingSources — все источники финансирования.
var FundingSources = []string{FundingBudget, FundingGrant, FundingContract, FundingOwn}

// Требуемая степень.
const (
	DegreeNone      = "none"      // не требуется
	DegreeCandidate = "candidate" // кандидат наук
	DegreeDoctor    = "doctor"    // доктор наук
)

// Degrees — все варианты требуемой степени.
var Degrees = []string{DegreeNone, DegreeCandidate, DegreeDoctor}

// Требуемое звание (только для ППС).
const (
	TitleNone      = "none"
	TitleDocent    = "docent"
	TitleProfessor = "professor"
)

// Titles — все варианты требуемого звания.
var Titles = []string{TitleNone, TitleDocent, TitleProfessor}

// Rates — допустимые ставки в процентах от полной.
var Rates = []int{25, 50, 75, 100}

// Пределы полей.
const (
	MinTitleLen     = 3
	MaxTitleLen     = 200
	MaxSummaryLen   = 600
	MinSummaryLen   = 20
	MaxDescription  = 10000
	MaxRequirements = 5000
	MaxFocusLen     = 300
	MaxCityLen      = 100
	MaxFundingNote  = 200
	MaxSalary       = 100_000_000
	MaxContractMon  = 120
	MaxSpecialties  = 5
)

// Input — поля формы вакансии, как их прислал человек.
type Input struct {
	Title         string
	PositionCode  string
	UnitID        *uuid.UUID
	Summary       string
	Description   string
	Requirements  string
	Focus         string
	CareerLevel   *int
	WorkFormat    string
	RegionCode    string
	City          string
	Housing       string
	RatePercent   *int
	SalaryFrom    *int
	SalaryTo      *int
	ContractType  string
	ContractMonth *int
	FundingSource string
	FundingNote   string
	Degree        string
	AcademicTitle string
	IsCompetition bool
	Deadline      string // 2026-11-14 или пусто
	Specialties   []string
}

// Ref — запись справочника: код и название.
type Ref struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// PositionRef — должность и её тип.
type PositionRef struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// OrgRef — организация вакансии.
type OrgRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	City string `json:"city"`
}

// UnitRef — подразделение вакансии.
type UnitRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Card — вакансия в списке.
type Card struct {
	ID             uuid.UUID   `json:"id"`
	Status         string      `json:"status"`
	Title          string      `json:"title"`
	Summary        string      `json:"summary"`
	Position       PositionRef `json:"position"`
	Organization   OrgRef      `json:"organization"`
	Unit           *UnitRef    `json:"unit"`
	City           string      `json:"city"`
	Region         *Ref        `json:"region"`
	WorkFormat     string      `json:"work_format"`
	CareerLevel    *int        `json:"career_level"`
	RatePercent    *int        `json:"rate_percent"`
	SalaryFrom     *int        `json:"salary_from"`
	SalaryTo       *int        `json:"salary_to"`
	ContractType   string      `json:"contract_type"`
	ContractMonths *int        `json:"contract_months"`
	IsCompetition  bool        `json:"is_competition"`
	Deadline       string      `json:"deadline"`
	Specialties    []Ref       `json:"specialties"`
	PublishedAt    *time.Time  `json:"published_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// Viewer — что смотрящий может с этой вакансией.
type Viewer struct {
	CanManage bool `json:"can_manage"`
	// Transitions — в какие статусы он может её перевести сейчас.
	Transitions []string `json:"transitions"`
}

// Detail — страница вакансии.
type Detail struct {
	Card
	Description   string    `json:"description"`
	Requirements  string    `json:"requirements"`
	Focus         string    `json:"focus"`
	Housing       string    `json:"housing"`
	FundingSource string    `json:"funding_source"`
	FundingNote   string    `json:"funding_note"`
	Degree        string    `json:"degree_required"`
	AcademicTitle string    `json:"title_required"`
	CreatedAt     time.Time `json:"created_at"`
	Viewer        Viewer    `json:"viewer"`
}

// List — страница списка вакансий.
type List struct {
	Items []Card `json:"items"`
	Total int    `json:"total"`
}

// MineList — «Мои вакансии»: страница списка и число вакансий по каждому статусу.
type MineList struct {
	Items  []Card         `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// Target — где человек может создать вакансию: вся организация или только перечисленные подразделения.
type Target struct {
	Organization OrgRef    `json:"organization"`
	WholeOrg     bool      `json:"whole_org"`
	Units        []UnitRef `json:"units"`
}
