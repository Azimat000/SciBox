package profiles

import (
	"time"

	"scibox/server/internal/career"
	"scibox/server/internal/privacy"
)

// careerLevel — уровень R1–R4 по степени и званию, как в подборе (matching.Profile.Level).
func careerLevel(degree, title string) int {
	return career.Level(degree, title)
}

// bestHIndex — лучшее из введённых значений h-index; 0, если ничего не указано.
func bestHIndex(h HIndex) int {
	best := 0
	for _, v := range []*int{h.RSCI, h.Scopus, h.WoS, h.Scholar} {
		if v != nil {
			best = max(best, *v)
		}
	}
	return best
}

// mask строит анонимный вид профиля (D-199). Вид собирается заново из разрешённого списка полей, а не вычитанием лишнего
// из полного: поле, добавленное в View позже, в анонимный вид само не попадёт.
//
// Разрешено: степень и её специальность, звание (без года), специальности, h-index, уровень R, число публикаций, грантов
// и патентов, счётчик статей Q1–Q2, отметка «открыт к предложениям». Запрещено всё остальное: имя, должность и место
// работы, город и регион, «о себе», тема диссертации, учреждение и год степени, идентификаторы, навыки, записи разделов
// (названия публикаций, гранты), контакты и приватность.
func mask(full View) View {
	out := View{
		ID:            full.ID,
		OpenToOffers:  full.OpenToOffers,
		Degree:        DegreeInfo{Level: full.Degree.Level, Specialty: full.Degree.Specialty},
		AcademicTitle: full.AcademicTitle,
		HIndex:        full.HIndex,
		Specialties:   full.Specialties,
		Skills:        Skills{Research: []string{}, General: []string{}},
		Sections:      emptySections(),
		Quartiles:     QuartileStats{Q12Total: full.Quartiles.Q12Total, Q12Recent: full.Quartiles.Q12Recent, RecentFrom: full.Quartiles.RecentFrom, Year: full.Quartiles.Year},
		CareerLevel:   full.CareerLevel,
		Counts:        full.Counts,
		// Точное время правки позволило бы сопоставить аноним с событиями, видимыми публично: оставляем только день.
		UpdatedAt: full.UpdatedAt.UTC().Truncate(24 * time.Hour),
	}
	codes := make([]string, 0, len(full.Specialties))
	for _, sp := range full.Specialties {
		codes = append(codes, sp.Code)
	}
	out.Label = privacy.MaskedLabel(privacy.MaskedFacts{
		Degree: full.Degree.Level, Level: full.CareerLevel, HIndex: bestHIndex(full.HIndex), Specialties: codes,
	})
	return out
}
