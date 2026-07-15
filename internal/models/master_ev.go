package models

import "time"

// MasterEV = บัญชีรุ่นรถ EV + ขนาดแบตเตอรี่ (master/catalog).
// แยกจาก Ev (รถที่ลูกค้าลงทะเบียนจริง ผูกกับ CA) โดยสิ้นเชิง — ตารางนี้เป็น
// read-only reference ให้ frontend ทำ dropdown cascade ยี่ห้อ→รุ่น→แบต.
// Grain = 1 แถวต่อ (brand, model, battery); รุ่นที่มีหลายก้อนแบตกระจายเป็นหลายแถว.
type MasterEV struct {
	ID    uint   `gorm:"primaryKey;autoIncrement"`
	Brand string `gorm:"type:varchar(150);not null;uniqueIndex:idx_master_ev_bmb,priority:1;index"` // dropdown ชั้น 1
	Model string `gorm:"type:varchar(255);not null;uniqueIndex:idx_master_ev_bmb,priority:2"`       // dropdown ชั้น 2

	// BatteryLabel = "ตัวเลขล้วน ไม่มีหน่วย" เช่น "60.48" หรือช่วง "1.3–1.6".
	// เป็นทั้ง option value ใน dropdown, ค่าที่เก็บลง evs.battery เมื่อเลือก, และ
	// join key ตอน resolve evs.master_ev_id — คอลัมน์เดียวนี้ load-bearing ทั้งหมด.
	// หน่วย "kWh" ให้ frontend เติมตอนแสดงผล.
	// สร้างจากตัวเลข (seeder) — อย่าใช้ label ดิบจาก JSON (mojibake).
	BatteryLabel string `gorm:"type:varchar(50);not null;uniqueIndex:idx_master_ev_bmb,priority:3"` // dropdown ชั้น 3

	CreatedAt time.Time
	UpdatedAt time.Time
}
