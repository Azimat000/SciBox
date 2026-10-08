// Package access решает, что человек может делать в организации. Это критичная зона (docs/TESTING.md):
// ошибка здесь открывает чужие вакансии и отклики. Пакет не знает про базу и HTTP; все остальные пакеты
// спрашивают права только здесь, своих проверок ролей нигде нет.
package access

import "github.com/google/uuid"

// Role — роль человека в организации.
type Role string

// Роли (D-005). Строки лежат в базе, не менять.
const (
	// RoleOwner — владелец: всё в организации.
	RoleOwner Role = "owner"
	// RoleHR — кадровик: вакансии и отклики всех подразделений.
	RoleHR Role = "hr"
	// RoleUnitHead — руководитель подразделения: вакансии и отклики только своих подразделений.
	RoleUnitHead Role = "unit_head"
)

// Roles — все роли в порядке от широких прав к узким.
var Roles = []Role{RoleOwner, RoleHR, RoleUnitHead}

// ParseRole превращает строку в роль; ok == false, если такой роли нет.
func ParseRole(s string) (Role, bool) {
	switch r := Role(s); r {
	case RoleOwner, RoleHR, RoleUnitHead:
		return r, true
	}
	return "", false
}

// Permission — действие, право на которое проверяется.
type Permission int

const (
	// EditOrganization — менять название, тип, город, сайт и описание организации.
	EditOrganization Permission = iota + 1
	// ManageMembers — приглашать сотрудников, менять роли, убирать из организации.
	ManageMembers
	// ManageUnits — создавать и удалять подразделения, назначать руководителя.
	ManageUnits
	// EditUnit — менять название, описание и темы подразделения. Привязано к подразделению.
	EditUnit
	// ManageVacancies — создавать, менять, публиковать и закрывать вакансии. Привязано к подразделению.
	ManageVacancies
	// ViewApplications — видеть отклики на вакансии. Привязано к подразделению.
	ViewApplications
)

// Actor — человек в организации: роль и подразделения, которыми он руководит.
// Нулевое значение — не сотрудник (ему нельзя ничего).
type Actor struct {
	Role   Role
	HeadOf []uuid.UUID
}

// NoUnit — «вакансия или действие без подразделения, на всю организацию».
var NoUnit = uuid.Nil

// Can отвечает, можно ли действие. unit нужен только для действий, привязанных к подразделению
// (EditUnit, ManageVacancies, ViewApplications); для остальных он игнорируется.
// Руководитель подразделения без подразделения (NoUnit) ничего сделать не может: вакансии на всю
// организацию ведут владелец и кадровик.
func (a Actor) Can(p Permission, unit uuid.UUID) bool {
	switch p {
	case EditOrganization, ManageMembers, ManageUnits:
		return a.Role == RoleOwner
	case EditUnit:
		return a.Role == RoleOwner || a.heads(unit)
	case ManageVacancies, ViewApplications:
		return a.Role == RoleOwner || a.Role == RoleHR || a.heads(unit)
	}
	return false
}

// heads: человек — руководитель этого подразделения.
func (a Actor) heads(unit uuid.UUID) bool {
	if a.Role != RoleUnitHead || unit == NoUnit {
		return false
	}
	for _, id := range a.HeadOf {
		if id == unit {
			return true
		}
	}
	return false
}

// IsMember — у человека есть роль в организации.
func (a Actor) IsMember() bool {
	_, ok := ParseRole(string(a.Role))
	return ok
}
