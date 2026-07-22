package models

import "time"

// MasterRole is the RBAC role catalog (operator, executive, ...). RulePolicy
// rows hang off RoleID to define what each role may do in the back-office.
type MasterRole struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	RoleName  string `gorm:"type:varchar(50);not null;uniqueIndex"` // "operator" | "executive"
	NameTh    string `gorm:"type:varchar(100)"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MasterUser is the back-office RBAC directory, seeded from PEA's HR master
// export (cmd/seedrbac). Permission checks key on DeptChangeCode — the same
// hr_dept_change_code claim Keycloak returns on login (see
// middleware.RBACMiddleware) — so access is department-scoped, not
// per-employee: any Keycloak login whose dept_change_code matches a row here
// gets that row's role, whether or not that specific employee_id was seeded.
type MasterUser struct {
	ID             uint       `gorm:"primaryKey;autoIncrement"`
	DeptChangeCode string     `gorm:"type:varchar(30);not null;index"`
	FullName       string     `gorm:"type:varchar(200)"`
	EmployeeId     string     `gorm:"type:varchar(20);not null;uniqueIndex"`
	RoleID         uint       `gorm:"not null"`
	Role           MasterRole `gorm:"foreignKey:RoleID"`
	Status         string     `gorm:"type:varchar(20);not null;default:active"` // active|inactive
	Description    string     `gorm:"type:varchar(255)"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// RulePolicy grants a (resource, action) pair to a role, e.g.
// (dashboard, read), (registration, write). RBACMiddleware requires a
// matching row before letting a request through.
type RulePolicy struct {
	ID        uint       `gorm:"primaryKey;autoIncrement"`
	RoleID    uint       `gorm:"not null;uniqueIndex:idx_rule_policy_role_resource_action"`
	Role      MasterRole `gorm:"foreignKey:RoleID"`
	Resource  string     `gorm:"type:varchar(50);not null;uniqueIndex:idx_rule_policy_role_resource_action"` // "dashboard" | "registration"
	Action    string     `gorm:"type:varchar(20);not null;uniqueIndex:idx_rule_policy_role_resource_action"` // "read" | "write"
	CreatedAt time.Time
	UpdatedAt time.Time
}
