// cmd/seedevmaster seeds the MasterEV catalog from ev_master_seed.json.
// Idempotent (ON CONFLICT (brand,model,battery_label) DO NOTHING).
// Run: go run ./cmd/seedevmaster
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm/clause"
)

//go:embed ev_master_seed.json
var dataFS embed.FS

type srcBrand struct {
	Name   string `json:"name"`
	Models []struct {
		Name      string `json:"name"`
		Batteries []struct {
			// เก็บแค่ตัวเลขที่ใช้ "สร้าง" BatteryLabel — capacity_kwh (เสมอ) +
			// capacity_max_kwh (เฉพาะรุ่นช่วง). usable_kwh/label ดิบไม่ใช้.
			CapacityKwh    float64  `json:"capacity_kwh"`
			CapacityMaxKwh *float64 `json:"capacity_max_kwh"`
		} `json:"batteries"`
	} `json:"models"`
}

func main() {
	_ = godotenv.Load()
	if err := database.Connect(); err != nil {
		log.Fatal(err)
	}
	if err := database.DB.AutoMigrate(&models.MasterEV{}); err != nil {
		log.Fatal(err)
	}

	raw, err := dataFS.ReadFile("ev_master_seed.json")
	if err != nil {
		log.Fatal(err)
	}
	var src []srcBrand
	if err := json.Unmarshal(raw, &src); err != nil {
		log.Fatal(err)
	}

	rows := make([]models.MasterEV, 0, 1024)
	for _, b := range src {
		brand := strings.TrimSpace(b.Name)
		for _, m := range b.Models {
			model := strings.TrimSpace(m.Name)
			for _, bat := range m.Batteries {
				rows = append(rows, models.MasterEV{
					Brand:        brand,
					Model:        model,
					BatteryLabel: batteryValue(bat.CapacityKwh, bat.CapacityMaxKwh),
				})
			}
		}
	}

	res := database.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "brand"}, {Name: "model"}, {Name: "battery_label"},
		},
		DoNothing: true,
	}).CreateInBatches(rows, 200)
	if res.Error != nil {
		log.Fatal(res.Error)
	}
	log.Printf("seeded %d source rows, %d inserted", len(rows), res.RowsAffected)
}

// batteryValue คืน "ตัวเลขล้วน ไม่มีหน่วย" — อย่าเชื่อ label ดิบใน JSON (mojibake).
// หน่วย kWh + คำอธิบาย เป็นหน้าที่ frontend ประกอบตอนแสดงผล (usable/max ส่งไปใน
// คอลัมน์ตัวเลขแยกอยู่แล้ว) — ที่นี่เก็บแค่เลขให้ evs.battery สะอาด.
func batteryValue(cap float64, max *float64) string {
	if max != nil {
		return fmt.Sprintf("%s–%s", fmtKwh(cap), fmtKwh(*max)) // ช่วง เช่น "1.3–1.6" (en-dash, ไม่มีหน่วย)
	}
	return fmtKwh(cap) // เช่น "83.9", "60.48", "80"
}

// fmtKwh: 80.0 → "80", 60.48 → "60.48", 42.2 → "42.2" (ตัด .0 ท้าย)
func fmtKwh(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
