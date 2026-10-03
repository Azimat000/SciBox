// Package profiles — профиль учёного: основные поля, разделы (образование, опыт, публикации, гранты, патенты,
// преподавание), приватность, поиск публикации по DOI и PDF с резюме (срез 7).
//
// Критичная зона (docs/TESTING.md): здесь решается, какие сведения о человеке уходят другим людям. Кто и что видит,
// решает пакет privacy; этот пакет собирает сведения о смотрящем и убирает из ответа то, что ему не положено.
package profiles

import (
	"time"

	"github.com/google/uuid"
)

// Виды записей разделов. Строки лежат в базе.
const (
	KindEducation   = "education"
	KindExperience  = "experience"
	KindPublication = "publication"
	KindGrant       = "grant"
	KindPatent      = "patent"
	KindTeaching    = "teaching"
)

// Kinds — все виды записей в порядке показа на странице.
var Kinds = []string{KindEducation, KindExperience, KindPublication, KindGrant, KindPatent, KindTeaching}

// Степень и звание (те же значения, что в вакансиях).
const (
	DegreeNone      = "none"
	DegreeCandidate = "candidate"
	DegreeDoctor    = "doctor"

	TitleNone      = "none"
	TitleDocent    = "docent"
	TitleProfessor = "professor"
)

var (
	degrees = []string{DegreeNone, DegreeCandidate, DegreeDoctor}
	titles  = []string{TitleNone, TitleDocent, TitleProfessor}
)

// Типы публикаций (к ним сводятся и типы Crossref).
var pubTypes = []string{"article", "book", "chapter", "conference", "preprint", "thesis", "other"}

// Откуда взята запись о публикации.
const (
	SourceManual   = "manual"
	SourceCrossref = "crossref"
)

var (
	grantRoles   = []string{"lead", "participant"}
	patentTypes  = []string{"invention", "utility_model", "software", "database", "other"}
	teachLevels  = []string{"bachelor", "master", "postgraduate", "continuing", "other"}
	pubSources   = []string{SourceManual, SourceCrossref}
	maxItemsKind = map[string]int{
		KindEducation: 30, KindExperience: 60, KindPublication: 300, KindGrant: 60, KindPatent: 60, KindTeaching: 60,
	}
)

// MaxSpecialties — сколько научных специальностей можно указать в профиле.
const MaxSpecialties = 5

// sortOngoing — год для сортировки незавершённых записей: они идут первыми.
const sortOngoing = 9999

// ItemFields — поля записи любого раздела. Каждый вид использует свои; остальные при сохранении отбрасываются.
type ItemFields struct {
	// Образование.
	Institution string `json:"institution,omitempty"` // также преподавание
	Program     string `json:"program,omitempty"`
	// Опыт работы.
	Organization string `json:"organization,omitempty"`
	Position     string `json:"position,omitempty"`
	// Публикации, гранты, патенты.
	Title   string `json:"title,omitempty"`
	Authors string `json:"authors,omitempty"` // публикации и патенты
	// Публикации.
	Venue   string `json:"venue,omitempty"`
	PubType string `json:"pub_type,omitempty"`
	DOI     string `json:"doi,omitempty"`
	URL     string `json:"url,omitempty"`
	Volume  string `json:"volume,omitempty"`
	Issue   string `json:"issue,omitempty"`
	Pages   string `json:"pages,omitempty"`
	Source  string `json:"source,omitempty"`
	// Гранты.
	Funder string `json:"funder,omitempty"`
	Role   string `json:"role,omitempty"`
	// Гранты и патенты.
	Number string `json:"number,omitempty"`
	// Патенты.
	PatentType string `json:"patent_type,omitempty"`
	Office     string `json:"office,omitempty"`
	// Преподавание.
	Course string `json:"course,omitempty"`
	Level  string `json:"level,omitempty"`
	// Общие.
	Description string `json:"description,omitempty"`
	Year        *int   `json:"year,omitempty"`      // публикация, патент
	YearFrom    *int   `json:"year_from,omitempty"` // образование, опыт, грант, преподавание
	YearTo      *int   `json:"year_to,omitempty"`   // нет — «по настоящее время»
}

// ItemInput — запись, как её прислал человек.
type ItemInput struct {
	Kind string `json:"kind"`
	ItemFields
}

// Item — сохранённая запись.
type Item struct {
	ID   uuid.UUID `json:"id"`
	Kind string    `json:"kind"`
	ItemFields
}

// CoreInput — основные поля профиля, как их прислал человек.
type CoreInput struct {
	Headline          string   `json:"headline"`
	City              string   `json:"city"`
	RegionCode        string   `json:"region_code"`
	About             string   `json:"about"`
	Degree            string   `json:"degree"`
	DegreeSpecialty   string   `json:"degree_specialty_code"`
	DegreeYear        *int     `json:"degree_year"`
	DegreeInstitution string   `json:"degree_institution"`
	Dissertation      string   `json:"dissertation_title"`
	AcademicTitle     string   `json:"academic_title"`
	AcademicTitleYear *int     `json:"academic_title_year"`
	ORCID             string   `json:"orcid"`
	SPIN              string   `json:"spin"`
	ScopusID          string   `json:"scopus_id"`
	WosID             string   `json:"wos_id"`
	HRsci             *int     `json:"h_rsci"`
	HScopus           *int     `json:"h_scopus"`
	HWos              *int     `json:"h_wos"`
	HScholar          *int     `json:"h_scholar"`
	ContactEmail      string   `json:"contact_email"`
	Specialties       []string `json:"specialties"`
}

// Code — запись справочника: номер и название.
type Code struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// DegreeInfo — учёная степень.
type DegreeInfo struct {
	Level        string `json:"level"`
	Specialty    *Code  `json:"specialty"`
	Year         *int   `json:"year"`
	Institution  string `json:"institution"`
	Dissertation string `json:"dissertation"`
}

// Identifiers — идентификаторы учёного в научных базах.
type Identifiers struct {
	ORCID    string `json:"orcid"`
	SPIN     string `json:"spin"`
	ScopusID string `json:"scopus_id"`
	WosID    string `json:"wos_id"`
}

// HIndex — h-index по базам, введённый вручную.
type HIndex struct {
	RSCI    *int `json:"rsci"`
	Scopus  *int `json:"scopus"`
	WoS     *int `json:"wos"`
	Scholar *int `json:"scholar"`
}

// Sections — записи разделов; пустые разделы — пустые списки, не null.
type Sections struct {
	Education    []Item `json:"education"`
	Experience   []Item `json:"experience"`
	Publications []Item `json:"publications"`
	Grants       []Item `json:"grants"`
	Patents      []Item `json:"patents"`
	Teaching     []Item `json:"teaching"`
}

// View — профиль так, как его видит смотрящий: чего ему видеть не положено, в ответе нет.
type View struct {
	ID                uuid.UUID   `json:"id"`
	Name              string      `json:"name"`
	Visibility        string      `json:"visibility,omitempty"` // только владельцу
	OpenToOffers      bool        `json:"open_to_offers"`
	Headline          string      `json:"headline"`
	City              string      `json:"city"`
	Region            *Code       `json:"region"`
	About             string      `json:"about"`
	Degree            DegreeInfo  `json:"degree"`
	AcademicTitle     string      `json:"academic_title"`
	AcademicTitleYear *int        `json:"academic_title_year"`
	Identifiers       Identifiers `json:"identifiers"`
	HIndex            HIndex      `json:"h_index"`
	Specialties       []Code      `json:"specialties"`
	ContactEmail      string      `json:"contact_email,omitempty"` // только владельцу и сотрудникам организаций
	Sections          Sections    `json:"sections"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

// ViewerInfo — что смотрящему можно: страница сама решает, какие кнопки показать.
type ViewerInfo struct {
	IsOwner        bool `json:"is_owner"`
	CanSeeContacts bool `json:"can_see_contacts"`
}

// Page — ответ «профиль» целиком.
type Page struct {
	Profile View       `json:"profile"`
	Viewer  ViewerInfo `json:"viewer"`
}
