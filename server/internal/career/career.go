// Package career — карьерный уровень учёного R1–R4 (как в EURAXESS) по степени и званию. Им пользуются подбор вакансий и
// анонимные карточки (срез 15), поэтому правило живёт в одном месте.
package career

// Level — без степени R1, кандидат наук R2, доктор наук R3, доктор наук с учёным званием профессора R4. Это оценка для
// подсказок, а не утверждение о человеке. Неизвестные значения считаются как «нет».
func Level(degree, title string) int {
	switch {
	case degree == "doctor" && title == "professor":
		return 4
	case degree == "doctor":
		return 3
	case degree == "candidate":
		return 2
	}
	return 1
}
