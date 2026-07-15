package models

import "time"

// GeneralInfo is a per-CA registration record. Ca is globally unique (one
// electricity account = one registration); PID (the citizen who registered
// it) is intentionally NOT unique — one ThaID citizen can register multiple
// CA numbers (e.g. multiple properties they hold).
type GeneralInfo struct {
	ID uint `gorm:"primaryKey;autoIncrement"`
	// column:pid explicit — GORM's default namer turns the all-caps field
	// "PID" into "p_id", which silently breaks any raw Where("pid = ?", ...) query.
	PID       string `gorm:"column:pid;type:varchar(13);not null;index"` // from ThaID session — never client input
	FirstName string `gorm:"not null"`
	LastName  string `gorm:"not null"`
	Address   string `gorm:"not null"`
	Ca        string `gorm:"not null;uniqueIndex"`

	// EntrySource is the channel (auth/service.EntrySource: "smartplus" or
	// "thaid") that most recently wrote this row — set on first registration
	// and refreshed on every edit (edits are gated on PID ownership only, see
	// ReService.ErrNotOwner). Kept as a plain string so this base models
	// package doesn't depend on the auth package.
	EntrySource string `gorm:"column:entry_source;type:varchar(20);not null;default:'thaid'"`

	Chargers []Charger `gorm:"foreignKey:GeneralInfoID"`
	Evs      []Ev      `gorm:"foreignKey:GeneralInfoID"`

	// WattdId links this registration to a Watt-D account (entered by the
	// citizen during the wizard, optional) — having one is a precondition for
	// earning Watt-D Points (see internal/admin's points engine). Nil = not
	// linked, submitted straight via ThaID without Smart Plus/Watt-D.
	WattdId *string `gorm:"column:wattd_id;type:varchar(50)"`

	// RegistrantName is the back-office's editable "ผู้ลงทะเบียน" display name,
	// distinct from FirstName/LastName (which is the CA *owner*'s name from
	// PEA's customer master — the two can differ, e.g. a tenant registering a
	// CA billed to the landlord). Empty until a staff member edits it or it's
	// captured at submission time; the admin package falls back to
	// FirstName+LastName when empty.
	RegistrantName string `gorm:"column:registrant_name;type:varchar(200)"`

	// PEA branch + business-type fields from cs-service's /customer/detail —
	// captured for grid-planning reporting (design/07-reports-analytics.md),
	// not surfaced in any UI yet. Only populated on first creation (cs-service
	// isn't re-queried on edit, since the CA itself doesn't change).
	PeaName          string `gorm:"column:pea_name;type:varchar(100)"`           // เช่น "กฟจ.ตรัง"
	CaName           string `gorm:"column:ca_name;type:varchar(200)"`            // ชื่อเจ้าของ CA แบบเต็มจาก cs-service (ต่างจาก FirstName/LastName ที่แยกคำ)
	PeaOffice        string `gorm:"column:pea_office;type:varchar(20)"`          // รหัสเขต เช่น "KTRU"
	BpNo             string `gorm:"column:bp_no;type:varchar(50)"`               // Business Partner No
	BusinessType     string `gorm:"column:business_type;type:varchar(20)"`       // เช่น "TSIC"
	BusinessTypeCode string `gorm:"column:business_type_code;type:varchar(20)"`  // เช่น "00001"
	BusinessTypeText string `gorm:"column:business_type_text;type:varchar(200)"` // เช่น "บ้านอยู่อาศัย"

	Status     string `gorm:"type:varchar(20);not null;default:'pending'"` // pending|approved|rejected|needs_info
	ReviewedBy string `gorm:"type:varchar(100)"`                           // Employee.Sub, nullable
	ReviewedAt *time.Time

	// Notes/checklist/PointsAwarded are the back-office review layer added on
	// top of the citizen's submission (internal/admin) — nil PointsAwarded
	// means "not decided yet"; 0 means "approved but not eligible for points".
	Notes            string `gorm:"type:text"`
	ChecklistReg     bool
	ChecklistCharger bool
	ChecklistEv      bool
	PointsAwarded    *int

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Charger struct {
	ID            uint `gorm:"primaryKey;autoIncrement"`
	GeneralInfoID uint
	GeneralInfo   GeneralInfo `gorm:"foreignKey:GeneralInfoID;references:ID"`

	VendorID uint
	Vendor   Vendor `gorm:"foreignKey:VendorID;references:ID"`

	SerialNumber  string `gorm:"type:varchar(100);not null;uniqueIndex"`
	ConnectorType string `gorm:"type:varchar(20);not null"`
	Kw            string `gorm:"type:varchar(20);not null"`
	// S3 object keys, not URLs — this storage backend (Dell EMC ECS) has no
	// anonymous public read, so a stored "public URL" would never work; a
	// short-lived signed URL must be generated on demand (storage.PresignGet).
	// Columns stay named image_url/label_image_url (pre-existing data, no migration needed).
	ImageKey      string `gorm:"column:image_url;type:text;not null"`       // device photo
	LabelImageKey string `gorm:"column:label_image_url;type:text;not null"` // spec/serial label photo
	Brand         string `gorm:"type:varchar(100)"`
	Model         string `gorm:"type:varchar(100)"`

	// MasterChargerID = ลิงก์ไปบัญชี master (nullable). set ตอน create เมื่อ brand+model
	// ตรงกับแถวใน master_chargers (ค่าปกติมาจาก dropdown จึงตรงเป๊ะ); null = ตู้นอกบัญชี catalog.
	MasterChargerID *uint          `gorm:"column:master_charger_id;index"`
	MasterCharger   *MasterCharger `gorm:"foreignKey:MasterChargerID;references:ID"`
}

// Vendor covers both charger and EV vendors, distinguished by Type ("charger"|"ev").
type Vendor struct {
	ID         uint   `gorm:"primaryKey;autoIncrement"`
	VendorName string `gorm:"not null;index:idx_vendor_name_type,unique"`
	Country    string
	Type       string `gorm:"type:varchar(10);not null;index:idx_vendor_name_type,unique"`

	Chargers []Charger `gorm:"foreignKey:VendorID"`
	Evs      []Ev      `gorm:"foreignKey:VendorID"`
}

type Ev struct {
	ID            uint `gorm:"primaryKey;autoIncrement"`
	GeneralInfoID uint
	GeneralInfo   GeneralInfo `gorm:"foreignKey:GeneralInfoID;references:ID"`

	VendorID uint
	Vendor   Vendor `gorm:"foreignKey:VendorID;references:ID"`

	Brand   string
	Model   string
	Year    string `gorm:"type:varchar(4);not null"`
	Battery string `gorm:"type:varchar(50);not null"`

	ChargingPeriod     string `gorm:"type:varchar(255);not null"`
	ChargingStartTime  string `gorm:"type:varchar(5);not null"` // HH:mm
	ChargingFinishTime string `gorm:"type:varchar(5);not null"` // HH:mm

	// MasterEvID = ลิงก์ไปบัญชี master (nullable). set ตอน create เมื่อ brand+model+battery
	// ตรงกับแถวใน master_evs (ค่าปกติมาจาก dropdown จึงตรงเป๊ะ); null = รถนอกบัญชี catalog.
	MasterEvID *uint     `gorm:"column:master_ev_id;index"`
	MasterEV   *MasterEV `gorm:"foreignKey:MasterEvID;references:ID"`
}
