package models

import "time"

type GeneralInfo struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	FirstName string    `gorm:"not null;"`
	LastName  string    `gorm:"not null"`
	Address   string    `gorm:"not null"`
	Ca        string    `gorm:"not null"`
	Chargers  []Charger `gorm:"foreignKey:UserID"`
	Evs       []Ev      `gorm:"foreignKey:UserID"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Charger struct {
	ID uint `gorm:"primaryKey;autoIncrement"`

	UserID uint
	User   GeneralInfo `gorm:"foreignKey:UserID;references:ID;"`

	VendorID uint
	Vendor   VendorCharge `gorm:"foreignKey:VendorID;references:ID"`

	SerialNumber string `gorm:"type:varchar(100);not null;uniqueIndex"`
}

type VendorCharge struct {
	ID         uint `gorm:"primaryKey;autoIncrement"`
	VendorName string
	Country    string

	Chargers []Charger `gorm:"foreignKey:VendorID"`
}

type Ev struct {
	ID uint `gorm:"primaryKey;autoIncrement"`

	UserID uint
	User   GeneralInfo `gorm:"foreignKey:UserID;references:ID;"`

	PlateNumber string `gorm:"type:varchar(20);not null"`
	Province    string
	Brand       string
	Model       string

	VendorID uint
	Vendor   VendorEv `gorm:"foreignKey:VendorID;references:ID"`
}

type VendorEv struct {
	ID         uint `gorm:"primaryKey;autoIncrement"`
	VendorName string
	Country    string

	Evs []Ev `gorm:"foreignKey:VendorID"`
}

// type User struct {
// 	ID        uint   `gorm:"primaryKey"`
// 	Email     string `gorm:"uniqueIndex;not null"`
// 	Username  string `gorm:"uniqueIndex;not null"`
// 	FirstName string
// 	LastName  string

// }
