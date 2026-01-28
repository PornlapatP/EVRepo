package models

import "time"

type User struct {
	ID        uint   `gorm:"primaryKey"`
	Email     string `gorm:"uniqueIndex;not null"`
	Username  string `gorm:"uniqueIndex;not null"`
	FirstName string
	LastName  string
	CreatedAt time.Time
	UpdatedAt time.Time
}
