package applications

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
	"scibox/server/internal/orgs"
	"scibox/server/internal/profiles"
)

// MaxExportRows — сколько откликов попадает в один файл. Больше организации за один раз не нужно (отбор по вакансии
// и статусу уменьшает список), а предел держит стоимость выгрузки и размер файла (срез 16, D-205).
const MaxExportRows = 5000

// exportLimit — действующий предел; тест уменьшает его, чтобы не создавать пять тысяч откликов.
var exportLimit = MaxExportRows

// ExportRow — отклик одной строкой файла. Данные берутся из самого отклика: имя из аккаунта (у стёртого аккаунта оно
// заменено), контакты и профиль из снимка, который человек отправил организации (D-079). Профиль соискателя, каким он
// стал после отклика, в выгрузку не попадает.
type ExportRow struct {
	ID              uuid.UUID
	ApplicantName   string
	ContactEmail    string
	VacancyTitle    string
	OrgName         string
	UnitName        string
	Status          string
	CreatedAt       time.Time
	StatusChangedAt time.Time
	// Из снимка профиля; пусто у обезличенного отклика (аккаунт удалён, D-142).
	Degree        string
	AcademicTitle string
	Level         int
	HIndex        *int
}

// Export — строки выгрузки и признак «в списке было больше».
type Export struct {
	Rows      []ExportRow
	Truncated bool
}

// ExportCandidates собирает выгрузку откликов по тем же правилам, что и список откликов организации (Candidates):
// права access.ViewApplications на вакансию, отборы по вакансии и статусу. Выгрузка записывается в журнал действий
// каждой организации, чьи отклики в неё вошли, в той же транзакции: нет записи — нет выгрузки.
func (s *Service) ExportCandidates(ctx context.Context, user auth.User, f CandidateFilter) (Export, error) {
	if f.Status != "" && !isOneOf(f.Status, Statuses) {
		return Export{}, &auth.ValidationError{Fields: map[string]string{"status": "Такого статуса нет"}}
	}
	var out Export
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		sc, err := orgs.ScopeOf(ctx, q, user.ID, access.ViewApplications)
		if err != nil {
			return err
		}
		list, err := q.ExportCandidates(ctx, dbgen.ExportCandidatesParams{
			WholeOrgs: sc.WholeOrgs, Units: sc.Units, VacancyID: f.VacancyID, Status: f.Status, RowLimit: num.Int32(exportLimit + 1),
		})
		if err != nil {
			return fmt.Errorf("applications: export candidates: %w", err)
		}
		if len(list) > exportLimit {
			list, out.Truncated = list[:exportLimit], true
		}
		out.Rows = make([]ExportRow, 0, len(list))
		perOrg := map[uuid.UUID]int{}
		for _, r := range list {
			out.Rows = append(out.Rows, exportRowOf(r))
			perOrg[r.OrgID]++
		}
		return s.recordExport(ctx, q, user, f, list, perOrg)
	})
	if err != nil {
		return Export{}, err
	}
	return out, nil
}

// recordExport пишет по записи на каждую организацию, чьи отклики вошли в файл. Пустая выгрузка ничего не раскрывает
// и в журнал не пишется.
func (s *Service) recordExport(ctx context.Context, q *dbgen.Queries, user auth.User, f CandidateFilter, list []dbgen.ExportCandidatesRow, perOrg map[uuid.UUID]int) error {
	done := map[uuid.UUID]bool{}
	for _, r := range list {
		if done[r.OrgID] {
			continue
		}
		done[r.OrgID] = true
		e := audit.Event{
			Action: audit.ApplicationsExported, OrgID: r.OrgID, Actor: user.ID,
			Details: map[string]string{"rows": strconv.Itoa(perOrg[r.OrgID]), "scope": "list"},
		}
		if f.Status != "" {
			e.Details["status"] = f.Status
		}
		if f.VacancyID != uuid.Nil {
			e.TargetKind, e.TargetID, e.TargetLabel = "vacancy", f.VacancyID, r.VacancyTitle
			e.Details["scope"] = "vacancy"
		}
		if err := audit.Record(ctx, q, e); err != nil {
			return err
		}
	}
	return nil
}

func exportRowOf(r dbgen.ExportCandidatesRow) ExportRow {
	row := ExportRow{
		ID: r.ID, ApplicantName: r.ApplicantName, ContactEmail: r.ContactEmail, VacancyTitle: r.VacancyTitle, OrgName: r.OrgName,
		Status: r.Status, CreatedAt: r.CreatedAt, StatusChangedAt: r.StatusChangedAt,
	}
	if r.UnitName != nil {
		row.UnitName = *r.UnitName
	}
	// Обезличенный отклик хранит «{}»: степень, звание и показатели пусты. Снимок, который не разобрать, ведёт себя так же:
	// выгрузка не должна падать из-за одной повреждённой строки.
	var snap profiles.View
	if r.Anonymized || json.Unmarshal(r.Profile, &snap) != nil {
		return row
	}
	row.Degree, row.AcademicTitle = snap.Degree.Level, snap.AcademicTitle
	if row.Degree != "" || row.AcademicTitle != "" {
		row.Level = profiles.CareerLevel(row.Degree, row.AcademicTitle)
	}
	for _, h := range []*int{snap.HIndex.RSCI, snap.HIndex.Scopus, snap.HIndex.WoS, snap.HIndex.Scholar} {
		if h != nil && (row.HIndex == nil || *h > *row.HIndex) {
			v := *h
			row.HIndex = &v
		}
	}
	return row
}

// ---- файл ----

// exportHeader — заголовки столбцов: по-русски, как их увидит человек в Excel.
var exportHeader = []string{
	"Имя", "Почта для связи", "Вакансия", "Организация", "Подразделение", "Статус", "Дата отклика", "Статус изменён",
	"Степень", "Звание", "Уровень", "h-index", "Ссылка на отклик",
}

var (
	exportStatus = map[string]string{
		StatusSent: "Отправлен", StatusViewed: "Просмотрен", StatusRejected: "Отказ",
		StatusAccepted: "Принят", StatusWithdrawn: "Отозван",
	}
	exportDegree = map[string]string{profiles.DegreeNone: "Нет", profiles.DegreeCandidate: "Кандидат наук", profiles.DegreeDoctor: "Доктор наук"}
	exportTitle  = map[string]string{profiles.TitleNone: "Нет", profiles.TitleDocent: "Доцент", profiles.TitleProfessor: "Профессор"}
)

// exportTime — время для людей: «14.10.2026 09:30», по Москве, как везде на сайте (D-089).
func exportTime(t time.Time) string { return t.In(moscow).Format("02.01.2006 15:04") }

// cell обезвреживает ячейку: Excel и Calc читают текст, начинающийся с «=», «+», «-», «@», табуляции или возврата каретки,
// как формулу. Имя и вакансию пишут люди, поэтому перед таким текстом ставится апостроф: в таблице он не виден, а формула
// не выполняется (срез 16, T-87).
func cell(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// WriteCSV пишет файл для Excel: UTF-8 с меткой BOM (без неё русская версия Excel покажет «кракозябры»), разделитель «;»
// (в русской раскладке Excel разделяет столбцы им, а не запятой), строки через CRLF. publicURL — адрес сайта для ссылок.
func WriteCSV(w io.Writer, rows []ExportRow, publicURL string) error {
	if _, err := io.WriteString(w, "\uFEFF"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma, cw.UseCRLF = ';', true
	if err := cw.Write(exportHeader); err != nil {
		return err
	}
	base := strings.TrimRight(publicURL, "/")
	for _, r := range rows {
		level, hindex := "", ""
		if r.Level > 0 {
			level = "R" + strconv.Itoa(r.Level)
		}
		if r.HIndex != nil {
			hindex = strconv.Itoa(*r.HIndex)
		}
		rec := []string{
			cell(r.ApplicantName), cell(r.ContactEmail), cell(r.VacancyTitle), cell(r.OrgName), cell(r.UnitName),
			exportStatus[r.Status], exportTime(r.CreatedAt), exportTime(r.StatusChangedAt),
			exportDegree[r.Degree], exportTitle[r.AcademicTitle], level, hindex, base + "/candidates/" + r.ID.String(),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
