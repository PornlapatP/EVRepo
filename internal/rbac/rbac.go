// Package rbac holds the lookup logic shared between RBACMiddleware (gate a
// route) and AdminService (surface the caller's role via GET /admin/me) so
// the two never drift apart.
package rbac

import (
	"errors"

	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm"
)

// ErrNoRole means the dept_change_code has no active MasterUser row —
// the caller has no back-office access at all.
var ErrNoRole = errors.New("no role for this dept_change_code")

// ResolveRole looks up the active MasterUser row for a Keycloak
// hr_dept_change_code claim and returns its role. Permission is
// department-scoped (see models.MasterUser doc comment): any login from a
// seeded department resolves to that department's role.
func ResolveRole(db *gorm.DB, deptChangeCode string) (models.MasterRole, error) {
	if deptChangeCode == "" {
		return models.MasterRole{}, ErrNoRole
	}
	var masterUser models.MasterUser
	err := db.Preload("Role").
		Where("dept_change_code = ? AND status = ?", deptChangeCode, "active").
		First(&masterUser).Error
	if err != nil {
		return models.MasterRole{}, ErrNoRole
	}
	return masterUser.Role, nil
}

// HasPermission reports whether roleID has been granted resource/action in RulePolicy.
func HasPermission(db *gorm.DB, roleID uint, resource, action string) bool {
	var count int64
	db.Model(&models.RulePolicy{}).
		Where("role_id = ? AND resource = ? AND action = ?", roleID, resource, action).
		Count(&count)
	return count > 0
}
