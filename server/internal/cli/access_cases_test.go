package cli

import (
	"net/http"

	"github.com/google/uuid"

	"scibox/server/internal/orgs"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

// P — параметры адреса.
type P = map[string]string

const (
	forbidden = http.StatusForbidden
	missing   = http.StatusNotFound
	badLink   = http.StatusBadRequest
)

func accessCases() []kase {
	var all []kase
	for _, group := range [][]kase{publicCases(), accountCases(), orgCases(), vacancyCases(), applicationCases(), offerCases(), profileCases(), personalCases()} {
		all = append(all, group...)
	}
	return all
}

func (f *fx) slug() P { return P{"slug": f.tm().Slug} }

// ---- адреса для всех ----

func publicCases() []kase {
	unknownLink := func(*fx, actor) call { return call{body: map[string]any{"token": "no-such-token"}} }
	return []kase{
		{route: "GET /api/health", kind: public},
		{route: "GET /api/landing", kind: public},
		{route: "GET /api/reference", kind: public},
		{route: "GET /api/vacancies", kind: public, make: func(*fx, actor) call { return call{query: "?limit=3"} }},
		{route: "GET /api/organizations/", kind: public},
		{route: "GET /api/scientists", kind: public},
		{route: "GET /api/organizations/{slug}/", kind: public, make: func(f *fx, _ actor) call { return call{params: f.slug()} }},
		{route: "GET /api/organizations/{slug}/units/{unitID}/", kind: public, make: func(f *fx, _ actor) call {
			return call{params: P{"slug": f.tm().Slug, "unitID": f.tm().UnitA.ID.String()}}
		}},
		{route: "GET /api/auth/me", kind: public},
		{route: "POST /api/auth/logout", kind: public},
		{route: "POST /api/auth/register", kind: public, make: func(*fx, actor) call {
			return call{body: map[string]any{"name": "Новый Человек", "email": "new-" + uuid.NewString()[:8] + "@example.ru", "password": "violet candle orchard 77", "consent": true}}
		}},
		{route: "POST /api/auth/login", kind: public, make: func(f *fx, _ actor) call {
			p := f.User()
			return call{body: map[string]any{"email": p.Email, "password": testkit.Password}}
		}},
		{route: "POST /api/auth/resend-confirmation", kind: public, make: func(*fx, actor) call { return call{body: map[string]any{"email": "nobody@example.ru"}} }},
		{route: "POST /api/auth/forgot-password", kind: public, make: func(*fx, actor) call { return call{body: map[string]any{"email": "nobody@example.ru"}} }},
		// Адреса по одноразовым ссылкам из писем: чужая ссылка не работает, человек не нужен.
		{route: "POST /api/auth/confirm-email", kind: public, ok: []int{badLink}, make: unknownLink},
		{route: "POST /api/auth/reset-password", kind: public, ok: []int{badLink}, make: func(*fx, actor) call {
			return call{body: map[string]any{"token": "no-such-token", "password": "violet candle orchard 77"}}
		}},
		{route: "POST /api/auth/confirm-email-change", kind: public, ok: []int{badLink}, make: unknownLink},
		{route: "POST /api/auth/revert-email-change", kind: public, ok: []int{badLink}, make: unknownLink},
		{route: "POST /api/invitations/lookup", kind: public, ok: []int{badLink}, make: unknownLink},
		{route: "POST /api/recommendations/lookup", name: "unknown link", kind: public, ok: []int{missing}, make: unknownLink},
		{route: "POST /api/recommendations/decline", name: "unknown link", kind: public, ok: []int{missing}, make: unknownLink},
		{route: "POST /api/recommendations/submit", name: "unknown link", kind: public, ok: []int{missing}, make: func(*fx, actor) call {
			return call{form: &formBody{data: map[string]any{"token": "no-such-token", "text": "Рекомендую."}}}
		}},
		{route: "POST /api/recommendations/lookup", name: "valid link", kind: public, make: func(f *fx, _ actor) call {
			_, token := f.reference(f.application().id)
			return call{body: map[string]any{"token": token}}
		}},
		{route: "POST /api/recommendations/decline", name: "valid link", kind: public, make: func(f *fx, _ actor) call {
			_, token := f.reference(f.application().id)
			return call{body: map[string]any{"token": token}}
		}},
		{route: "POST /api/recommendations/submit", name: "valid link", kind: public, make: func(f *fx, _ actor) call {
			_, token := f.reference(f.application().id)
			return call{form: &formBody{data: map[string]any{"token": token, "text": "Рекомендую без оговорок."}}}
		}},
	}
}

// ---- аккаунт: каждый работает со своим ----

func accountCases() []kase {
	body := func(m map[string]any) func(*fx, actor) call { return func(*fx, actor) call { return call{body: m} } }
	return []kase{
		{route: "PATCH /api/account/", kind: self, make: body(map[string]any{"name": "Новое Имя"})},
		{route: "POST /api/account/password", kind: self, make: body(map[string]any{"current_password": testkit.Password, "new_password": "fresh secure phrase 91"})},
		{route: "POST /api/account/reauth", kind: self, make: body(map[string]any{"password": testkit.Password})},
		{route: "GET /api/account/sessions", kind: self},
		{route: "POST /api/account/sessions/revoke-others", kind: self},
		{route: "POST /api/account/email", kind: self, make: body(map[string]any{"email": "changed-" + uuid.NewString()[:8] + "@example.ru"})},
		{route: "GET /api/account/deletion", kind: self},
		{route: "POST /api/account/deletion", kind: self},
		{route: "POST /api/account/deletion/cancel", kind: self},
		// Сеанс видит и завершает только его хозяин.
		{route: "DELETE /api/account/sessions/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.session().String()}}
		}},
	}
}

// ---- организации ----

func orgCases() []kase {
	unitA := func(f *fx, _ actor) call {
		return call{params: P{"slug": f.tm().Slug, "unitID": f.tm().UnitA.ID.String()}}
	}
	const publicPage = "страница организации и подразделения открыта всем"
	return []kase{
		{route: "POST /api/organizations/", kind: self, make: func(*fx, actor) call {
			return call{body: map[string]any{"name": "Новая организация " + uuid.NewString()[:8], "kind": orgs.KindInstitute, "city": "Томск"}}
		}},
		{route: "GET /api/my/organizations", kind: self},
		{route: "PATCH /api/organizations/{slug}/", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: f.slug(), body: map[string]any{"name": f.tm().Slug + " (новое название)", "kind": orgs.KindInstitute, "city": "Новосибирск"}}
		}},
		{route: "POST /api/organizations/{slug}/units/", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: f.slug(), body: map[string]any{"name": "Новая лаборатория", "kind": orgs.UnitLaboratory}}
		}},
		{route: "PATCH /api/organizations/{slug}/units/{unitID}/", kind: object, allow: of(owner, headA), deny: forbidden, why: publicPage, make: func(f *fx, who actor) call {
			c := unitA(f, who)
			c.body = map[string]any{"name": "Лаборатория катализа", "kind": orgs.UnitLaboratory, "description": "Изменено", "topics": []string{"Катализ"}}
			return c
		}},
		{route: "DELETE /api/organizations/{slug}/units/{unitID}/", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: P{"slug": f.tm().Slug, "unitID": f.unit().ID.String()}}
		}},
		{route: "PUT /api/organizations/{slug}/units/{unitID}/head", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: P{"slug": f.tm().Slug, "unitID": f.unit().ID.String()}, body: map[string]any{"user_id": f.tm().HR.ID}}
		}},
		{route: "GET /api/organizations/{slug}/members", kind: object, allow: of(owner), deny: forbidden, why: "список сотрудников закрыт, а сама организация открыта всем",
			make: func(f *fx, _ actor) call { return call{params: f.slug()} }},
		{route: "GET /api/organizations/{slug}/audit", kind: object, allow: of(owner), deny: forbidden, why: "журнал закрыт, а сама организация открыта всем",
			make: func(f *fx, _ actor) call { return call{params: f.slug()} }},
		{route: "PATCH /api/organizations/{slug}/members/{userID}", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: P{"slug": f.tm().Slug, "userID": f.member("unit_head").ID.String()}, body: map[string]any{"role": "hr"}}
		}},
		{route: "DELETE /api/organizations/{slug}/members/{userID}", name: "remove a colleague", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: P{"slug": f.tm().Slug, "userID": f.member("hr").ID.String()}}
		}},
		// Уйти из организации может каждый её сотрудник, сам. Сотрудники здесь свои у каждого запроса:
		// настоящий владелец после ухода перестал бы быть владельцем для следующих строк.
		{route: "DELETE /api/organizations/{slug}/members/{userID}", name: "leave yourself", kind: object, perActor: true, allow: of(owner, hr, headA, headB),
			deny: forbidden, why: publicPage, make: func(f *fx, who actor) call {
				roles := map[actor]string{owner: "owner", hr: "hr", headA: "unit_head", headB: "unit_head"}
				if role, ok := roles[who]; ok {
					p := f.member(role)
					return call{params: P{"slug": f.tm().Slug, "userID": p.ID.String()}, as: &p}
				}
				return call{params: P{"slug": f.tm().Slug, "userID": uuid.NewString()}}
			}},
		{route: "POST /api/organizations/{slug}/invitations", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: f.slug(), body: map[string]any{"email": "invited-" + uuid.NewString()[:8] + "@example.ru", "role": "hr"}}
		}},
		{route: "DELETE /api/organizations/{slug}/invitations/{id}", kind: object, allow: of(owner), deny: forbidden, why: publicPage, make: func(f *fx, _ actor) call {
			return call{params: P{"slug": f.tm().Slug, "id": f.orgInvitation("invited-" + uuid.NewString()[:8] + "@example.ru").String()}}
		}},
		{route: "POST /api/invitations/accept", kind: self, ok: []int{badLink}, make: func(*fx, actor) call { return call{body: map[string]any{"token": "no-such-token"}} }},
		// Принять приглашение по номеру может только тот, кому оно адресовано; для остальных его «нет» (T-16).
		{route: "POST /api/invitations/{id}/accept", kind: object, allow: of(subject), deny: badLink, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.orgInvitation(f.Subject().Email).String()}}
		}},
	}
}

// ---- вакансии ----

func vacancyCases() []kase {
	byID := func(status string) func(*fx, actor) call {
		return func(f *fx, _ actor) call { return call{params: P{"id": f.vacancy(status).String()}} }
	}
	const openVacancy = "опубликованная и закрытая вакансии открыты всем"
	return []kase{
		{route: "POST /api/vacancies", kind: object, allow: staffA, deny: forbidden, why: "организация открыта всем", make: func(f *fx, _ actor) call {
			return call{body: f.vacancyBody(true)}
		}},
		{route: "GET /api/my/vacancies", kind: self},
		{route: "GET /api/my/vacancy-targets", kind: self},
		{route: "GET /api/vacancies/{id}/", name: "published", kind: object, pub: true, allow: everyone, deny: missing, make: byID(vacancies.StatusPublished)},
		{route: "GET /api/vacancies/{id}/", name: "closed", kind: object, pub: true, allow: everyone, deny: missing, make: byID(vacancies.StatusClosed)},
		{route: "GET /api/vacancies/{id}/", name: "draft", kind: object, pub: true, allow: staffA, deny: missing, twin: []string{"id"}, make: byID(vacancies.StatusDraft)},
		{route: "GET /api/vacancies/{id}/", name: "archived", kind: object, pub: true, allow: staffA, deny: missing, twin: []string{"id"}, make: byID(vacancies.StatusArchived)},
		{route: "PATCH /api/vacancies/{id}/", name: "published", kind: object, allow: staffA, deny: forbidden, why: openVacancy, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusPublished).String()}, body: f.vacancyBody(false)}
		}},
		{route: "PATCH /api/vacancies/{id}/", name: "draft", kind: object, allow: staffA, deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusDraft).String()}, body: f.vacancyBody(false)}
		}},
		{route: "DELETE /api/vacancies/{id}/", kind: object, allow: staffA, deny: missing, twin: []string{"id"}, make: byID(vacancies.StatusDraft)},
		{route: "POST /api/vacancies/{id}/status", name: "close a published one", kind: object, allow: staffA, deny: forbidden, why: openVacancy, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusPublished).String()}, body: map[string]any{"status": vacancies.StatusClosed}}
		}},
		{route: "POST /api/vacancies/{id}/status", name: "publish a draft", kind: object, allow: staffA, deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusDraft).String()}, body: map[string]any{"status": vacancies.StatusPublished}}
		}},
		{route: "GET /api/applications/for-vacancy/{vacancyId}", name: "published", kind: object, allow: signedIn, deny: missing, make: func(f *fx, _ actor) call {
			return call{params: P{"vacancyId": f.vacancy(vacancies.StatusPublished).String()}}
		}},
		// Черновика для откликающегося «нет»: ответ тот же, что на выдуманный номер.
		{route: "GET /api/applications/for-vacancy/{vacancyId}", name: "draft", kind: object, deny: missing, twin: []string{"vacancyId"}, make: func(f *fx, _ actor) call {
			return call{params: P{"vacancyId": f.vacancy(vacancies.StatusDraft).String()}}
		}},
	}
}

// ---- отклики ----

func applicationCases() []kase {
	appOnly := func(f *fx, _ actor) call { return call{params: P{"id": f.application().id.String()}} }
	staffAndSubject := of(subject, owner, hr, headA)
	return []kase{
		{route: "POST /api/applications", kind: self, make: func(f *fx, _ actor) call {
			subj := f.Subject()
			return call{as: &subj, form: &formBody{
				data:  map[string]any{"vacancy_id": f.vacancy(vacancies.StatusPublished), "contact_email": subj.Email, "cover_letter": coverLetter, "referees": []any{}},
				files: [][2]string{pdfFile("diploma.pdf")},
			}}
		}},
		{route: "POST /api/applications", name: "draft vacancy", kind: object, deny: missing, only: of(guest, subject), // остальных останавливает пустой профиль, до вакансии дело не доходит
			twinCall: func(c call) call {
				data := map[string]any{}
				for k, v := range c.form.data.(map[string]any) {
					data[k] = v
				}
				data["vacancy_id"] = uuid.New()
				return call{form: &formBody{data: data, files: c.form.files}}
			}, make: func(f *fx, _ actor) call {
				return call{form: &formBody{
					data:  map[string]any{"vacancy_id": f.vacancy(vacancies.StatusDraft), "contact_email": f.Subject().Email, "cover_letter": coverLetter, "referees": []any{}},
					files: [][2]string{pdfFile("diploma.pdf")},
				}}
			}},
		{route: "GET /api/applications", kind: self},
		{route: "GET /api/my/candidates", kind: self},
		{route: "GET /api/my/candidate-vacancies", kind: self},
		{route: "GET /api/applications/{id}", kind: object, allow: staffAndSubject, deny: missing, twin: []string{"id"}, make: appOnly},
		{route: "GET /api/applications/{id}/files/{fileId}", name: "résumé", kind: object, allow: staffAndSubject, deny: missing, twin: []string{"id", "fileId"}, make: func(f *fx, _ actor) call {
			a := f.application()
			return call{params: P{"id": a.id.String(), "fileId": a.cv.String()}}
		}},
		{route: "GET /api/applications/{id}/files/{fileId}", name: "attachment", kind: object, allow: staffAndSubject, deny: missing, twin: []string{"id", "fileId"}, make: func(f *fx, _ actor) call {
			a := f.application()
			return call{params: P{"id": a.id.String(), "fileId": a.file.String()}}
		}},
		// Письмо рекомендателя видит только организация; для соискателя файла «нет».
		{route: "GET /api/applications/{id}/files/{fileId}", name: "reference letter", kind: object, allow: staffA, deny: missing, twin: []string{"id", "fileId"}, make: func(f *fx, _ actor) call {
			a := f.application().id
			_, token := f.reference(a)
			f.mustForm(testkit.Person{}, "/api/recommendations/submit", map[string]any{"token": token, "text": "Рекомендую."}, pdfFile("letter.pdf"))
			var fid uuid.UUID
			if err := f.w.pool.QueryRow(bgctx, `SELECT id FROM application_files WHERE application_id = $1 AND kind = 'reference_letter'`, a).Scan(&fid); err != nil {
				f.t.Fatal(err)
			}
			return call{params: P{"id": a.String(), "fileId": fid.String()}}
		}},
		{route: "POST /api/applications/{id}/withdraw", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: appOnly},
		{route: "POST /api/applications/{id}/status", kind: object, allow: staffA, deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.application().id.String()}, body: map[string]any{"status": "rejected", "note": "Не подходит"}}
		}},
		{route: "POST /api/applications/{id}/invitations", kind: object, allow: staffA, deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.application().id.String()}, body: map[string]any{"kind": "contacts", "message": "Пишите", "contact_name": "Ольга Кузнецова", "contact_email": "hr@example.ru"}}
		}},
		{route: "DELETE /api/applications/{id}/invitations/{invId}", kind: object, allow: staffA, deny: missing, twin: []string{"id", "invId"}, make: func(f *fx, _ actor) call {
			a := f.application().id
			return call{params: P{"id": a.String(), "invId": f.interview(a).String()}}
		}},
		{route: "POST /api/applications/{id}/invitations/{invId}/answer", kind: object, allow: of(subject), deny: missing, twin: []string{"id", "invId"}, make: func(f *fx, _ actor) call {
			a := f.application().id
			return call{params: P{"id": a.String(), "invId": f.interview(a).String()}, body: map[string]any{"action": "confirm"}}
		}},
		{route: "POST /api/applications/{id}/invitations/{invId}/accept-proposal", kind: object, allow: staffA, deny: missing, twin: []string{"id", "invId"}, make: func(f *fx, _ actor) call {
			a := f.application().id
			return call{params: P{"id": a.String(), "invId": f.proposal(a).String()}}
		}},
		{route: "POST /api/applications/{id}/references", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.application().id.String()}, body: map[string]any{"name": "Профессор Петров", "email": "ref-" + uuid.NewString()[:8] + "@example.ru", "relation": "руководитель"}}
		}},
		{route: "POST /api/applications/{id}/references/{refId}/resend", kind: object, allow: of(subject), deny: missing, twin: []string{"id", "refId"}, make: func(f *fx, _ actor) call {
			a := f.application().id
			ref, _ := f.reference(a)
			f.exec(`UPDATE reference_requests SET last_sent_at = now() - interval '2 days' WHERE id = $1`, ref) // повтор разрешён не раньше чем через сутки
			return call{params: P{"id": a.String(), "refId": ref.String()}}
		}},
		{route: "DELETE /api/applications/{id}/references/{refId}", kind: object, allow: of(subject), deny: missing, twin: []string{"id", "refId"}, make: func(f *fx, _ actor) call {
			a := f.application().id
			ref, _ := f.reference(a)
			return call{params: P{"id": a.String(), "refId": ref.String()}}
		}},
	}
}

// ---- приглашения учёных на вакансии ----

func offerCases() []kase {
	offerOnly := func(f *fx, _ actor) call {
		o, _ := f.offer()
		return call{params: P{"id": o.String()}}
	}
	return []kase{
		{route: "POST /api/offers", kind: object, allow: staffA, deny: missing, twinCall: func(c call) call {
			b := map[string]any{}
			for k, v := range c.body.(map[string]any) {
				b[k] = v
			}
			b["vacancy_id"] = uuid.New()
			return call{body: b}
		}, make: func(f *fx, _ actor) call {
			profile := f.scientist("public")
			return call{body: map[string]any{"vacancy_id": f.vacancy(vacancies.StatusPublished), "profile_id": profile, "message": "Приходите к нам"}}
		}},
		{route: "GET /api/offers", kind: self},
		{route: "GET /api/my/sent-offers", kind: self},
		{route: "GET /api/offers/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: offerOnly},
		{route: "POST /api/offers/{id}/answer", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			o, _ := f.offer()
			return call{params: P{"id": o.String()}, body: map[string]any{"action": "interested", "note": "Интересно"}}
		}},
		{route: "POST /api/offers/{id}/cancel", kind: object, allow: staffA, deny: missing, twin: []string{"id"}, make: offerOnly},
	}
}

// ---- люди и профили ----

func profileCases() []kase {
	byProfile := func(visibility string) func(*fx, actor) call {
		return func(f *fx, _ actor) call { return call{params: P{"id": f.scientist(visibility).String()}} }
	}
	// Кто видит профиль «виден организациям»: любой сотрудник любой организации, и сам хозяин.
	orgMembers := of(subject, owner, hr, headA, headB, foreign)
	return []kase{
		{route: "GET /api/scientists/{id}", name: "public", kind: object, pub: true, allow: everyone, deny: missing, make: byProfile("public")},
		{route: "GET /api/scientists/{id}", name: "visible to organizations", kind: object, pub: true, allow: orgMembers, deny: missing, twin: []string{"id"}, make: byProfile("orgs")},
		{route: "GET /api/scientists/{id}", name: "hidden", kind: object, pub: true, allow: of(subject), deny: missing, twin: []string{"id"}, make: byProfile("hidden")},
		{route: "GET /api/scientists/{id}/cv", name: "public", kind: object, allow: signedIn, deny: missing, make: byProfile("public")},
		{route: "GET /api/scientists/{id}/cv", name: "visible to organizations", kind: object, allow: orgMembers, deny: missing, twin: []string{"id"}, make: byProfile("orgs")},
		{route: "GET /api/scientists/{id}/cv", name: "hidden", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: byProfile("hidden")},
		{route: "GET /api/scientists/{id}/offer-targets", name: "public", kind: object, allow: signedIn, deny: missing, make: byProfile("public")},
		{route: "GET /api/scientists/{id}/offer-targets", name: "hidden", kind: object, deny: missing, twin: []string{"id"}, make: byProfile("hidden")},

		{route: "GET /api/profile/", kind: self},
		{route: "PUT /api/profile/", kind: self, make: func(*fx, actor) call {
			return call{body: map[string]any{"headline": "Научный сотрудник", "city": "Новосибирск", "region_code": "54", "about": "Изучаю катализаторы.", "specialties": []string{"1.4.4"}}}
		}},
		{route: "PUT /api/profile/privacy", kind: self, make: func(*fx, actor) call {
			return call{body: map[string]any{"visibility": "public", "open_to_offers": true}}
		}},
		{route: "POST /api/profile/items", kind: self, make: func(*fx, actor) call {
			return call{body: map[string]any{"kind": "publication", "title": "Статья", "authors": "Орлова Е.", "pub_type": "article", "year": 2023}}
		}},
		{route: "GET /api/profile/cv", kind: self, make: func(f *fx, _ actor) call { p := f.Subject(); return call{as: &p} }},
		{route: "GET /api/profile/doi", kind: self, ok: []int{http.StatusBadGateway}, make: func(*fx, actor) call { return call{query: "?doi=10.1234/abc"} }},
		{route: "GET /api/journals", kind: self, make: func(*fx, actor) call { return call{query: "?q=nature"} }},
		{route: "PUT /api/profile/items/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.profileItem().String()}, body: map[string]any{"title": "Статья (исправлено)", "authors": "Орлова Е.", "pub_type": "article", "year": 2023}}
		}},
		{route: "DELETE /api/profile/items/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.profileItem().String()}}
		}},
	}
}

// ---- личные списки: избранное, поиски, уведомления ----

func personalCases() []kase {
	return []kase{
		{route: "GET /api/favorites", kind: self},
		{route: "GET /api/favorites/ids", kind: self},
		{route: "GET /api/matches", kind: self},
		{route: "GET /api/deadlines", kind: self},
		{route: "GET /api/saved-searches", kind: self},
		{route: "POST /api/saved-searches", kind: self, make: func(*fx, actor) call {
			return call{body: map[string]any{"name": "Химия", "query": "q=химия", "frequency": "weekly"}}
		}},
		{route: "GET /api/notification-settings", kind: self},
		{route: "PUT /api/notification-settings", kind: self, make: func(*fx, actor) call {
			return call{body: map[string]any{"email_new_vacancies": true, "email_deadlines": false}}
		}},
		{route: "GET /api/notifications/", kind: self},
		{route: "GET /api/notifications/unread-count", kind: self},
		{route: "POST /api/notifications/read-all", kind: self},
		// В избранное кладут только открытую вакансию: черновика для всех «нет».
		{route: "PUT /api/favorites/{id}", name: "published", kind: object, allow: signedIn, deny: missing, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusPublished).String()}}
		}},
		{route: "PUT /api/favorites/{id}", name: "draft", kind: object, deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusDraft).String()}}
		}},
		{route: "DELETE /api/favorites/{id}", kind: object, allow: signedIn, deny: missing, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.vacancy(vacancies.StatusPublished).String()}}
		}},
		{route: "GET /api/saved-searches/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.savedSearch().String()}}
		}},
		{route: "PATCH /api/saved-searches/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.savedSearch().String()}, body: map[string]any{"name": "Химия (новое название)", "frequency": "instant"}}
		}},
		{route: "DELETE /api/saved-searches/{id}", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.savedSearch().String()}}
		}},
		{route: "POST /api/notifications/{id}/read", kind: object, allow: of(subject), deny: missing, twin: []string{"id"}, make: func(f *fx, _ actor) call {
			return call{params: P{"id": f.notification().String()}}
		}},
	}
}
