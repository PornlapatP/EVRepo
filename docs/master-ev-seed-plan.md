# Implementation Plan — MasterEV catalog + seed + API

> Handoff สำหรับ backend (EVRepo, Go 1.24 + GORM + Postgres)
> เป้าหมาย: seed บัญชี **รุ่นรถ EV + ขนาดแบตเตอรี่** เข้า DB และเปิด API ให้ frontend ทำ dropdown
> ผูกกันแบบ cascade `ยี่ห้อ → รุ่น → แบต` แทน mock ปัจจุบัน
> คู่กับ handoff ฝั่ง frontend: `ev-registration/docs/ev-catalog-implementation-plan.md`
> แนวเดียวกับ [`master-charger-seed-plan.md`](master-charger-seed-plan.md) (อ่านเทียบได้)

---

## 0. Decisions (ตัดสินแล้ว — ฝังในแผนนี้)

| # | ประเด็น | ข้อสรุป |
| --- | --- | --- |
| 1 | **ปี (Year)** | **ไม่อยู่ใน master/catalog และไม่แตะ backend catalog** — year เก็บที่ field เดิม **`models.Ev.Year`** (คอลัมน์ `year varchar(4)`, เก็บ **ค.ศ.** 4 หลัก เช่น `"2023"`) frontend เป็นคนสร้าง option ปีเอง (static) แล้วส่งค่ามาลง `evs.year` ตามเดิม · MasterEV **ไม่มีคอลัมน์ปี** |
| 2 | **เก็บ battery แบบไหน** | **string ตัวเลขล้วน (ไม่มีหน่วย)** — `BatteryLabel` เก็บแค่เลข เช่น `"60.48"`, ช่วงเก็บ `"1.3–1.6"` · **หน่วย `kWh` ให้ frontend เติมตอนแสดงผล** · ตอนลงทะเบียนจริง `evs.battery` เก็บ string ตัวเลขนี้ (ไม่ทำ id/FK) · **⚠️ อัปเดต 2026-07-15 (Plan A / minimal):** ตัดคอลัมน์ตัวเลขแยก `CapacityKwh`/`UsableKwh`/`CapacityMaxKwh` ออกทั้งหมด — ไม่มีใครใช้ (FE โชว์แค่ `label`) และ `usable_kwh` ก็ไม่ได้อยู่ใน label · ตารางเหลือแค่ `Brand/Model/BatteryLabel` · ถ้าอนาคตต้องโชว์ usable/filter เชิงเลข ค่อย re-seed จาก JSON (idempotent) |
| 3 | **ผูกเชิงโครงสร้าง (FK)** | **FK เฉพาะคำร้องใหม่** — เพิ่ม `evs.master_ev_id *uint` (nullable, FK → `master_evs`) · **resolve ตอน create** ด้วย (brand+model+battery) ที่มาจาก dropdown (ตรงเป๊ะอยู่แล้ว); จับไม่ได้ = `null` (รถนอกบัญชี) · **ไม่ backfill** แถวเก่า · **ไม่ seed `source_names`** (จำเป็นเฉพาะ backfill ซึ่งข้าม) · รายละเอียด §8 |

---

## 1. บริบท / ขอบเขต

- นี่คือ **ตารางอ้างอิง (master/catalog)** — แยกจาก `models.Ev` (รถที่ลูกค้าลงทะเบียนจริง ผูกกับ CA) โดยสิ้นเชิง **ห้ามปน**
- แหล่งข้อมูล: `ev_master_seed.json` (nested — `brand → models[] → batteries[]`) ที่ทีมส่งให้ **ใช้ไฟล์นี้**
- Grain ที่ตกลง: **1 แถวต่อ (brand, model, battery)** — ตารางแบนตัวเดียว (แนวเดียวกับ MasterCharger) เพราะ 1 รุ่นมีได้หลายก้อนแบต (เช่น `AION Y` มี 2 ก้อน) → กระจายเป็นหลายแถว
- Scope งานนี้ = **model + migrate + seeder + API** · การแก้ฟอร์มฝั่ง frontend เป็น PR แยก (ดู §9 + doc frontend)

---

## 2. Model

เพิ่มไฟล์ `internal/models/master_ev.go`:

```go
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
	// เป็นทั้ง option value ใน dropdown, ค่าที่เก็บลง evs.battery, และ join key
	// ตอน resolve evs.master_ev_id — คอลัมน์เดียวนี้ load-bearing ทั้งหมด.
	// หน่วย "kWh" ให้ frontend เติมตอนแสดงผล.
	// สร้างจากตัวเลข (§5) — อย่าใช้ label ดิบจาก JSON (mojibake, ดู §5).
	BatteryLabel string `gorm:"type:varchar(50);not null;uniqueIndex:idx_master_ev_bmb,priority:3"` // dropdown ชั้น 3

	CreatedAt time.Time
	UpdatedAt time.Time
}
```

> unique key = **(brand, model, battery_label)** → idempotent + กันแบตซ้ำในรุ่นเดียว
>
> **⚠️ Plan A (minimal, 2026-07-15):** ตัด `CapacityKwh`/`UsableKwh`/`CapacityMaxKwh` ออกแล้ว (Decision #2) —
> seeder ยังอ่าน `capacity_kwh`/`capacity_max_kwh` จาก JSON เพื่อ "สร้าง" `BatteryLabel` แต่ไม่เก็บเป็นคอลัมน์

---

## 3. Migrate

เพิ่ม `&models.MasterEV{}` เข้า `AutoMigrate(...)` ที่ [`cmd/server/main.go:30`](../cmd/server/main.go):

```go
database.DB.AutoMigrate(
	&models.GeneralInfo{}, &models.Charger{}, &models.Vendor{}, &models.Ev{},
	&models.Employee{}, &models.AuditLog{},
	&models.MasterEV{}, // เพิ่ม (MasterCharger ก็เพิ่มด้วยถ้ายังไม่ merge)
)
```

Seeder (§4) เรียก `AutoMigrate(&models.MasterEV{})` ซ้ำได้ (idempotent) — server เป็นเจ้าของ schema จริง

---

## 4. Seeder — `cmd/seedevmaster/main.go`

ตาม pattern `cmd/seedmaster` / `cmd/mockcitizen` (standalone cmd + `godotenv.Load()` + `database.Connect()`).
Idempotent ด้วย GORM `clause.OnConflict` บน (brand, model, battery_label).

**ต้องก๊อป `ev_master_seed.json` มาไว้ที่ `cmd/seedevmaster/ev_master_seed.json`** — `go:embed` ข้าม `..` ไม่ได้

```go
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
			CapacityKwh    float64  `json:"capacity_kwh"`
			UsableKwh      *float64 `json:"usable_kwh"`
			CapacityMaxKwh *float64 `json:"capacity_max_kwh"`
			// Label ดิบ — จงใจไม่ใช้ (mojibake) สร้างใหม่ด้วย buildLabel แทน
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
					Brand:          brand,
					Model:          model,
					BatteryLabel:   batteryValue(bat.CapacityKwh, bat.CapacityMaxKwh),
					CapacityKwh:    bat.CapacityKwh,
					UsableKwh:      bat.UsableKwh,
					CapacityMaxKwh: bat.CapacityMaxKwh,
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

// batteryValue คืน "ตัวเลขล้วน ไม่มีหน่วย" — อย่าเชื่อ label ดิบใน JSON (mojibake, §5).
// หน่วย kWh + คำอธิบาย "(แบตใช้งานจริง …)" เป็นหน้าที่ frontend ประกอบตอนแสดงผล
// (usable/max ส่งไปในคอลัมน์ตัวเลขแยกอยู่แล้ว) — ที่นี่เก็บแค่เลขให้ evs.battery สะอาด.
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
```

**Deps:** `gorm.io/gorm/clause`, `github.com/joho/godotenv` — มีอยู่แล้ว ไม่ต้องเพิ่ม dependency

---

## 5. Transform rules (สรุป)

| กฎ | รายละเอียด |
| --- | --- |
| **ไม่ใช้ label ดิบ** | ⚠️ **อย่าใช้ field `label` จาก JSON** — mojibake (BMW `"83.9 kWh (à¹à¸...)"`, Lexus `"1.3â1.6 kWh"`) เกิดจาก UTF-8 ถูกอ่านเป็น Latin-1 · `batteryValue` สร้างใหม่จากตัวเลข |
| **BatteryLabel = ตัวเลขล้วน** | มี `capacity_max_kwh` → ช่วง `"{cap}–{max}"` (เช่น `"1.3–1.6"`) · ไม่งั้น → `"{cap}"` (เช่น `"83.9"`) · **ไม่ใส่หน่วย `kWh` และไม่ใส่คำอธิบายไทย** — FE เติมเอง |
| usable/max | **Plan A: ไม่เก็บ** — `usable_kwh` ทิ้ง, `capacity_max_kwh` ใช้แค่ตอน seed เพื่อสร้าง label ช่วง แล้วทิ้ง (ไม่มีคอลัมน์) |
| format ตัวเลข | `strconv.FormatFloat(v,'f',-1,64)` — `80.0→"80"`, `60.48→"60.48"` |
| dedupe | `ON CONFLICT (brand,model,battery_label) DO NOTHING` — แถวแรกในไฟล์ชนะ |
| brand ทับซ้อน (ไม่ใช่ bug) | `AVATR` มีทั้ง brand เดี่ยว + ใต้ `CHANGAN` (model `"AVATR 07"`) · `DENZA D9` ใต้ทั้ง `BYD` + brand `DENZA` → **seed ตามที่มา** ค่า brand ต่างกันจึงไม่ชน |
| ไม่เก็บ | `source_names` (Decision #3), `label` ดิบ, และ **ไม่มีคอลัมน์ year** (Decision #1) |

---

## 6. Data QA — ตรวจก่อน merge

```sql
-- จำนวนแถวรวม (≈ ผลรวมของทุก battery ในทุกรุ่น)
SELECT count(*) FROM master_evs;

-- battery_label ต้องเป็น "ตัวเลขล้วน" — ต้องไม่มี "kWh" และไม่มี mojibake/ตัวไทยหลุดมา
SELECT count(*) FROM master_evs WHERE battery_label ~* 'kwh|à|â|แบต';  -- คาด 0

-- รุ่นแบบช่วง — label ต้องเป็น "cap–max" ไม่มีหน่วย (Plan A: ไม่มีคอลัมน์ตัวเลขแล้ว เช็คที่ label)
SELECT brand, model, battery_label FROM master_evs
WHERE battery_label LIKE '%–%' ORDER BY brand LIMIT 20;  -- คาด "1.3–1.6", "86.1–281.9"

-- brand/รุ่นที่มีหลายก้อนแบต (ต้องเห็นหลายแถว เช่น AION Y = 2)
SELECT brand, model, count(*) FROM master_evs GROUP BY 1,2 HAVING count(*) > 1 ORDER BY 3 DESC;
```
รัน seeder **ซ้ำ** → `inserted 0` (ยืนยัน idempotent)

---

## 7. API — `GET /api/v1/ev-catalog`

module ใหม่ `internal/catalog/{service.go,controller.go}` (mirror pattern `registration`).

- **สาธารณะ (ไม่ผูก auth)** — ข้อมูลอ้างอิงรถ ไม่ sensitive, cache ได้ · วางใน group `/api/v1` นอก `citizen`/`staff` · เพิ่ม `Cache-Control` (เช่น `public, max-age=3600`)
- อ่าน `master_evs` ทั้งก้อน (`ORDER BY brand, model, id`) แล้ว **nest ใน Go** เป็น `brand → models → batteries`
- register: `apiV1.GET("/ev-catalog", catalogController.Get)` ที่ [`cmd/server/main.go`](../cmd/server/main.go)

**Response (JSON tag ให้ frontend consume ตรง — camelCase):**
```jsonc
{
  "data": [
    {
      "brand": "BYD",
      "models": [
        {
          "model": "ATTO 3",
          "batteries": [
            // label = ตัวเลขล้วน (ไม่มีหน่วย) · FE แสดง `${label} kWh` = "60.48 kWh"
            // Plan A: battery มี field เดียวคือ label (ตัด capacity/usable/max ออกแล้ว)
            { "label": "60.48" }
          ]
        }
      ]
    }
  ]
}
```

**DTO (`internal/catalog/service.go` หรือ `model.go`):**
```go
// Plan A (minimal): battery มี field เดียว — label. ตัด capacity/usable/max ออกแล้ว.
type BatteryDTO struct {
	Label string `json:"label"`
}
type ModelDTO struct {
	Model     string       `json:"model"`
	Batteries []BatteryDTO `json:"batteries"`
}
type BrandDTO struct {
	Brand  string     `json:"brand"`
	Models []ModelDTO `json:"models"`
}
// controller: SELECT * ORDER BY brand, model, id → group ตามลำดับ (เรียงแล้วจึง group ต่อเนื่องได้)
```

> Nesting ทำใน service ด้วย loop เดียว (แถวเรียง brand→model แล้ว) — ไม่ต้อง N+1 / ไม่ต้อง Preload

---

## 8. ผูก `evs` → `master_ev` (FK เฉพาะคำร้องใหม่) — Decision #3

hard link จาก รถที่ลงทะเบียนจริง → บัญชี master · **resolve ตอน create เท่านั้น, ไม่ backfill**

### 8.1 เพิ่มคอลัมน์บน `Ev` model
[`internal/models/registration.go`](../internal/models/registration.go) — struct `Ev`:
```go
// MasterEvID = ลิงก์ไปบัญชี master (nullable). set ตอน create เมื่อ brand+model+battery
// ตรงกับแถวใน master_evs (ค่าปกติมาจาก dropdown จึงตรงเป๊ะ); null = รถนอกบัญชี catalog.
MasterEvID *uint     `gorm:"column:master_ev_id;index"`
MasterEV   *MasterEV `gorm:"foreignKey:MasterEvID;references:ID"`
```
`Ev` อยู่ใน `AutoMigrate` อยู่แล้ว → เพิ่มคอลัมน์อัตโนมัติตอน server boot (nullable ไม่กระทบแถวเดิม)

### 8.2 resolve ตอน create — ReService
ที่ `CreateGeneralInfoWithRelations` ลูปสร้าง `ev` ([`ReService/service.go:203`](../internal/registration/ReService/service.go)) เพิ่ม lookup ก่อน `tx.Create`:
```go
// resolve FK จาก catalog — match ด้วย 3 คีย์ที่มาจาก dropdown (ตรงเป๊ะอยู่แล้ว)
var masterID *uint
var m models.MasterEV
if err := tx.Select("id").
    Where("brand = ? AND model = ? AND battery_label = ?", e.Brand, e.Model, e.Battery).
    First(&m).Error; err == nil {
    masterID = &m.ID
} // ErrRecordNotFound → masterID = nil (นอกบัญชี, ยอมรับได้)
ev.MasterEvID = masterID
```
- `e.Battery` = ตัวเลขล้วน (`"60.48"`) ตรงกับ `master_evs.battery_label` (สอดคล้อง Decision #2)
- **ไม่ backfill** แถวเก่า → เก่าคง null (ต้องการทีหลังค่อย seed `source_names` + เขียน backfill script แยก)
- edit path (`tx.Model(...).Updates(ev)`): เซ็ต `ev.MasterEvID` ก่อนได้เหมือนกัน — แต่ระวัง GORM `Updates(struct)` **ข้าม field ที่เป็น nil pointer**; ถ้าต้องบังคับให้กลับเป็น null ต้องเติม `.Select("master_ev_id")`

### 8.3 verify
```sql
SELECT count(*) FILTER (WHERE master_ev_id IS NOT NULL) AS linked,
       count(*) FILTER (WHERE master_ev_id IS NULL)     AS off_catalog
FROM evs;   -- คำร้องใหม่จาก dropdown ควร linked; null = รถนอกบัญชี/แถวเก่า
```

---

## 9. Run & verify

```bash
# 1) ก๊อปไฟล์ข้อมูลเข้า package dir ของ seeder
cp ev_master_seed.json EVRepo/cmd/seedevmaster/ev_master_seed.json

# 2) postgres รันอยู่ (docker-compose: container "postgres", DB "Test_PEA_DB", user "pea_user")
docker compose up -d postgres

# 3) รัน seeder (อ่าน DB_* จาก .env)
cd EVRepo && go run ./cmd/seedevmaster

# 4) รัน server แล้วยิง endpoint
go run ./cmd/server
curl -s localhost:8080/api/v1/ev-catalog | jq '.data[0]'   # เห็น nested + label ไทยสะอาด
```

---

## 10. Downstream (PR แยก — ฝั่ง frontend)

รายละเอียดเต็มใน `ev-registration/docs/ev-catalog-implementation-plan.md` §5 — สรุปสัญญา (contract) ที่ backend ต้องคงไว้:
- endpoint `GET /api/v1/ev-catalog` + response shape §7 (camelCase, nested)
- `battery` ที่ frontend ส่งกลับมาตอนลงทะเบียน = **`label` (ตัวเลขล้วน ไม่มีหน่วย)** ตรงจาก catalog (Decision #2) → ลง `evs.battery` เดิม
  · ⚠️ downstream: `evs.battery` จะเป็นเลขล้วน (เช่น `"60.48"`) — backoffice ที่โชว์ค่านี้ควรเติม `" kWh"` ตอนแสดงผลถ้าต้องการหน่วย
- `year` frontend ส่งมาเอง (ค.ศ. 4 หลัก) → ลง `evs.year` เดิม (Decision #1) — backend ไม่ต้องแก้อะไรเรื่องปี

---

## Deliverables checklist

- [x] `internal/models/master_ev.go` (§2)
- [x] เพิ่ม `MasterEV` ใน `AutoMigrate` ที่ `cmd/server/main.go` (§3)
- [x] `cmd/seedevmaster/main.go` + `cmd/seedevmaster/ev_master_seed.json` (§4)
- [x] `internal/catalog/{service.go,controller.go}` + register route (§7)
- [x] `evs.master_ev_id` FK บน `Ev` model + resolve ตอน create ใน ReService (§8)
- [x] `go build ./...` ผ่าน + รัน seeder (524 rows) + verify label สะอาด + `curl` endpoint (§6, §9)
- [x] รัน seeder ซ้ำ → `inserted 0` (idempotent) · resolve query ตรงค่า catalog จริง (§8.3)
