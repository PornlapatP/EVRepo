package models

import "time"

// Employee is a local cache of PEA staff who have logged in via Keycloak,
// populated on each successful Keycloak callback. Supports GeneralInfo.ReviewedBy.
type Employee struct {
	ID           uint   `gorm:"primaryKey;autoIncrement"`
	Sub          string `gorm:"type:varchar(100);not null;uniqueIndex"` // Keycloak sub
	Email        string
	GivenName    string
	FamilyName   string
	HrEmployeeId string
	HrDepartment string
	HrPosition   string

	CreatedAt time.Time
	UpdatedAt time.Time
}
