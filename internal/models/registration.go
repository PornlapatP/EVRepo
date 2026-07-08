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
	// and refreshed on every edit (edits are only ever done via smartplus, see
	// ReService.ErrEditForbidden). Kept as a plain string so this base models
	// package doesn't depend on the auth package.
	EntrySource string `gorm:"column:entry_source;type:varchar(20);not null;default:'thaid'"`

	Chargers []Charger `gorm:"foreignKey:GeneralInfoID"`
	Evs      []Ev      `gorm:"foreignKey:GeneralInfoID"`

	Status     string `gorm:"type:varchar(20);not null;default:'pending'"` // pending|approved|rejected
	ReviewedBy string `gorm:"type:varchar(100)"`                           // Employee.Sub, nullable
	ReviewedAt *time.Time

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

	// PlateNumber/Province are not collected by the frontend's registration
	// wizard today (only brand/model/year/battery/charging schedule are) —
	// kept nullable so the schema doesn't require data that never arrives.
	PlateNumber string `gorm:"type:varchar(20)"`
	Province    string
	Brand       string
	Model       string
	Year        string `gorm:"type:varchar(4);not null"`
	Battery     string `gorm:"type:varchar(50);not null"`

	ChargingPeriod     string `gorm:"type:varchar(255);not null"`
	ChargingStartTime  string `gorm:"type:varchar(5);not null"` // HH:mm
	ChargingFinishTime string `gorm:"type:varchar(5);not null"` // HH:mm
}
