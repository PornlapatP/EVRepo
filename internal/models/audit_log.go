package models

import "time"

// AuditLog is the back-office's append-only trail of every staff action
// against a GeneralInfo — inline field edits (with mandatory reason),
// checklist toggles, and decisions (approve/reject/needs_info). Doubles as
// the "activity log" shown in the review UI (internal/admin renders Text
// straight into ActivityEntry.text).
type AuditLog struct {
	ID            uint `gorm:"primaryKey;autoIncrement"`
	GeneralInfoID uint `gorm:"not null;index"`

	// ActorSub/ActorName come from the Keycloak session (Employee.Sub /
	// GivenName+FamilyName) — never trusted from the request body.
	ActorSub  string `gorm:"type:varchar(100);not null"`
	ActorName string `gorm:"type:varchar(200);not null"`

	Action string `gorm:"type:varchar(30);not null"` // patch_reg|patch_charger|patch_ev|checklist|decision
	Reason string `gorm:"type:text"`
	Note   string `gorm:"type:text"`
	Text   string `gorm:"type:text;not null"` // human-readable Thai description shown in the activity log

	CreatedAt time.Time
}
