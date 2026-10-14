package profiles

import (
	"slices"
	"testing"

	"scibox/server/internal/skills"
)

// useSkillsDictionary кладёт в справочник несколько навыков с редкими названиями (база общая на пакет, другие тесты
// пишут «Excel» и «Английский B2») и убирает их по окончании теста.
func useSkillsDictionary(t *testing.T) {
	t.Helper()
	exec := func(sql string) {
		t.Helper()
		if _, err := sharedPool.Exec(bg, sql); err != nil {
			t.Fatalf("skills dictionary: %v", err)
		}
	}
	exec(`INSERT INTO skills (id, kind, grp, name) VALUES
	        (900001, 'research', 'Методы', 'Тестовая хроматография'),
	        (900002, 'research', 'Методы', 'Тестовый учёт проб'),
	        (900003, 'general', 'Языки', 'Тестовый язык')`)
	exec(`INSERT INTO skill_terms (kind, key, term, words, skill_id, is_name) VALUES
	        ('research', 'тестовая хроматография', 'Тестовая хроматография', 'тестовая хроматография', 900001, true),
	        ('research', 'тх-тест', 'ТХ-тест', 'тх тест', 900001, false),
	        ('research', 'тестовый учет проб', 'Тестовый учёт проб', 'тестовый учет проб', 900002, true),
	        ('general', 'тестовый язык', 'Тестовый язык', 'тестовый язык', 900003, true),
	        ('general', 'ття', 'ТТЯ', 'ття', 900003, false)`)
	t.Cleanup(func() { exec(`DELETE FROM skills WHERE id >= 900001`) })
}

func TestSaveCoreBringsSkillsToTheDictionarySpelling(t *testing.T) {
	useSkillsDictionary(t)
	w := newWorld(t)
	p := w.user("Елена Орлова")
	in := CoreInput{
		ResearchSkills: []string{"тх-тест", "Моя редкая методика", "ТЕСТОВЫЙ УЧЕТ ПРОБ", "Тестовая Хроматография", "тестовый учёт проб"},
		GeneralSkills:  []string{"ття", "Тестовый язык", "Excel", "Другое умение"},
	}
	page, err := w.svc.SaveCore(bg, p.User, in)
	if err != nil {
		t.Fatal(err)
	}
	// Синонимы сводятся к названию, повторы после приведения убираются, свои навыки остаются как набраны, порядок человека.
	if want := []string{"Тестовая хроматография", "Моя редкая методика", "Тестовый учёт проб"}; !slices.Equal(page.Profile.Skills.Research, want) {
		t.Errorf("научные: %q, want %q", page.Profile.Skills.Research, want)
	}
	if want := []string{"Тестовый язык", "Excel", "Другое умение"}; !slices.Equal(page.Profile.Skills.General, want) {
		t.Errorf("общие: %q, want %q", page.Profile.Skills.General, want)
	}
	// Такое же значение лежит и в таблице, и в тексте поиска: профиль хранит строки.
	var research []string
	if err := sharedPool.QueryRow(bg, `SELECT research_skills FROM profiles WHERE user_id = $1`, p.User.ID).Scan(&research); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(research, page.Profile.Skills.Research) {
		t.Errorf("в базе: %q", research)
	}
}

// Прежние навыки, которых нет в справочнике (или справочник сменился), при следующем сохранении не пропадают.
func TestSaveCoreKeepsSkillsThatTheDictionaryDoesNotKnow(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена Орлова")
	in := CoreInput{ResearchSkills: []string{"ИК-спектроскопия", "рентгеновская дифракция"}, GeneralSkills: []string{"Английский B2"}}
	first, err := w.svc.SaveCore(bg, p.User, in)
	if err != nil {
		t.Fatal(err)
	}
	useSkillsDictionary(t)
	second, err := w.svc.SaveCore(bg, p.User, in)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first.Profile.Skills.Research, second.Profile.Skills.Research) || !slices.Equal(second.Profile.Skills.General, []string{"Английский B2"}) {
		t.Errorf("%+v → %+v", first.Profile.Skills, second.Profile.Skills)
	}
}

// Навык, вписанный человеком, остаётся в его профиле и не попадает в чужие подсказки (D-212, I-108).
func TestOwnSkillsNeverReachTheSuggestions(t *testing.T) {
	useSkillsDictionary(t)
	w := newWorld(t)
	p := w.user("Елена Орлова")
	const own = "Криоэлектронная ультрамикротомия личная"
	if _, err := w.svc.SaveCore(bg, p.User, CoreInput{ResearchSkills: []string{own}, GeneralSkills: []string{own}}); err != nil {
		t.Fatal(err)
	}
	svc := skills.NewService(sharedPool)
	for _, kind := range []string{skills.KindResearch, skills.KindGeneral} {
		for _, q := range []string{"ультрамикротомия", own, "Криоэлектронная"} {
			got, err := svc.Search(bg, kind, q, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Items) != 0 {
				t.Errorf("%s %q: %+v", kind, q, got.Items)
			}
		}
	}
	if n := countRows(t, `SELECT count(*) FROM skill_terms WHERE key LIKE '%ультрамикротомия%'`); n != 0 {
		t.Errorf("the dictionary grew by %d own skills", n)
	}
}
