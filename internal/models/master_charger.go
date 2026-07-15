package models

import "time"

// MasterCharger = บัญชีรุ่นเครื่องชาร์จที่ผ่านการรับรอง (master/catalog).
// แยกจาก Charger (ตู้ที่ลูกค้าลงทะเบียนจริง ผูกกับ CA) โดยสิ้นเชิง — ตารางนี้เป็น
// read-only reference ให้ frontend ทำ dropdown + auto-fill spec.
type MasterCharger struct {
	ID    uint   `gorm:"primaryKey;autoIncrement"`
	Brand string `gorm:"type:varchar(150);not null;uniqueIndex:idx_master_brand_model;index"` // dropdown ชั้น 1
	Model string `gorm:"type:varchar(255);not null;uniqueIndex:idx_master_brand_model"`       // dropdown ชั้น 2

	// CurrentType = ประเภทกระแส "AC" | "DC" — map ตรงกับ field ชื่อ (สับสน) `connectorType`
	// ในฟอร์ม frontend ที่รับ enum AC/DC เท่านั้น → ใช้ค่านี้ auto-fill.
	CurrentType string `gorm:"type:varchar(2);not null"`
	// ConnectorType = หัวกายภาพ normalized: "CCS2" | "CHAdeMO" | "AC_TYPE_2".
	// ฟอร์มปัจจุบัน "ยังไม่มีช่อง" ให้เลือกหัวกายภาพ — เก็บไว้เผื่ออนาคต.
	ConnectorType   string `gorm:"type:varchar(20);not null"`
	TotalConnectors int    `gorm:"not null"`
	RatedPowerKw    string `gorm:"type:varchar(20);not null"` // = totalRatedPowerKw → auto-fill form.kw

	CreatedAt time.Time
	UpdatedAt time.Time
}
