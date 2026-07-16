# Implementation Plan — MasterCharger catalog + seed

> Handoff สำหรับ backend (EVRepo, Go 1.24 + GORM + Postgres)
> เป้าหมาย: seed บัญชี **รุ่นเครื่องชาร์จที่ผ่านการรับรอง** (464 รุ่น) เข้า DB เพื่อให้ frontend
> ทำ dropdown เลือก `brand → model` + auto-fill spec แทนการพิมพ์มือ

---

## 1. บริบท / ขอบเขต

- นี่คือ **ตารางอ้างอิง (master/catalog)** — แยกจาก `models.Charger` (ตู้ที่ลูกค้าลงทะเบียนจริง ผูกกับ CA) โดยสิ้นเชิง **ห้ามปน**
- แหล่งข้อมูล: `chargers.json` (nested — 1 รุ่นมีหลาย connector) ที่ทีมส่งให้ **ใช้ไฟล์นี้ อย่าใช้ CSV** (CSV encoding เพี้ยน)
- Grain ที่ตกลง: **ระดับรุ่น (model-level) 1 ตาราง** — ยุบ connector หลายหัวให้เหลือค่าเดียวต่อรุ่น
- Scope งานนี้ = **model + migrate + seeder + FK ผูก `chargers → master_charger`** (§7) · API/endpoint และการแก้ฟอร์มฝั่ง frontend เป็น PR แยก (ดู §9)

---

## 2. Model

เพิ่มไฟล์ `internal/models/master_charger.go`:

```go
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
```

**Field mapping (สำคัญ — กันตั้งชื่อสับสน):**

| ฟอร์ม frontend | ค่า | มาจาก MasterCharger |
| --- | --- | --- |
| `connectorType` (label "Connector type") | `AC` / `DC` | **`CurrentType`** ✅ |
| `kw` | "7.4"/"11"/"22"/อื่น | `RatedPowerKw` |
| — (ยังไม่มีช่อง) | CCS2/CHAdeMO/… | `ConnectorType` |

> ⚠️ อย่าเผลอเอา `MasterCharger.ConnectorType` (CCS2…) ไปใส่ `form.connectorType` — enum ฝั่งฟอร์มรับแค่ AC/DC จะพังทันที

---

## 3. Migrate

เพิ่ม `&models.MasterCharger{}` เข้า `AutoMigrate(...)` ที่ [`cmd/server/main.go:30`](../cmd/server/main.go):

```go
database.DB.AutoMigrate(
	&models.GeneralInfo{}, &models.Charger{}, &models.Vendor{}, &models.Ev{},
	&models.Employee{}, &models.AuditLog{},
	&models.MasterCharger{}, // เพิ่ม
)
```

Seeder (§4) ก็เรียก `AutoMigrate(&models.MasterCharger{})` ซ้ำได้ (idempotent) — server เป็นเจ้าของ schema จริง

---

## 4. Seeder — `cmd/seedmaster/main.go`

ตาม pattern `cmd/mockcitizen` (standalone cmd + `godotenv.Load()` + `database.Connect()`). Idempotent ด้วย GORM `clause.OnConflict` บน (brand, model).

**ต้องก๊อป `chargers.json` มาไว้ที่ `cmd/seedmaster/chargers.json`** — `go:embed` ข้าม `..` ไม่ได้

```go
// cmd/seedmaster seeds the MasterCharger catalog from the certified-charger list.
// Idempotent (ON CONFLICT (brand,model) DO NOTHING).  Run: go run ./cmd/seedmaster
package main

import (
	"embed"
	"encoding/json"
	"log"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm/clause"
)

//go:embed chargers.json
var dataFS embed.FS

type srcCharger struct {
	Brand           string      `json:"brand"`
	Model           string      `json:"model"`
	TotalConnectors int         `json:"totalConnectors"`
	TotalRatedKw    json.Number `json:"totalRatedPowerKw"`
	Connectors      []struct {
		CurrentType   string `json:"currentType"`
		ConnectorType string `json:"connectorType"`
	} `json:"connectors"`
}

func main() {
	_ = godotenv.Load()
	if err := database.Connect(); err != nil {
		log.Fatal(err)
	}
	if err := database.DB.AutoMigrate(&models.MasterCharger{}); err != nil {
		log.Fatal(err)
	}

	raw, err := dataFS.ReadFile("chargers.json")
	if err != nil {
		log.Fatal(err)
	}
	var src []srcCharger
	if err := json.Unmarshal(raw, &src); err != nil {
		log.Fatal(err)
	}

	rows := make([]models.MasterCharger, 0, len(src))
	for _, c := range src {
		rows = append(rows, models.MasterCharger{
			Brand:           canonBrand(c.Brand),
			Model:           strings.TrimSpace(c.Model),
			CurrentType:     collapseCurrentType(c),
			ConnectorType:   collapseConnectorType(c),
			TotalConnectors: c.TotalConnectors,
			RatedPowerKw:    c.TotalRatedKw.String(),
		})
	}

	res := database.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "brand"}, {Name: "model"}},
		DoNothing: true,
	}).CreateInBatches(rows, 100)
	if res.Error != nil {
		log.Fatal(res.Error)
	}
	log.Printf("seeded %d source rows, %d inserted", len(rows), res.RowsAffected)
}

// DC ถ้ามีหัว DC สักตัว, ไม่งั้น AC
func collapseCurrentType(c srcCharger) string {
	for _, con := range c.Connectors {
		if strings.EqualFold(con.CurrentType, "DC") {
			return "DC"
		}
	}
	return "AC"
}

// เลือกหัว DC ก่อน (เด่นสุด) ไม่งั้นหัวแรก แล้ว normalize
func collapseConnectorType(c srcCharger) string {
	for _, con := range c.Connectors {
		if strings.EqualFold(con.CurrentType, "DC") {
			return normConnector(con.ConnectorType)
		}
	}
	if len(c.Connectors) > 0 {
		return normConnector(c.Connectors[0].ConnectorType)
	}
	return ""
}

func normConnector(s string) string {
	u := strings.ToUpper(strings.ReplaceAll(s, " ", "")) // "CCS 2","C CS2" → "CCS2"
	switch {
	case strings.Contains(u, "CHADEMO"):
		return "CHAdeMO"
	case strings.Contains(u, "CCS"):
		return "CCS2"
	case strings.Contains(u, "TYPE2"), strings.Contains(u, "TYPR2"):
		return "AC_TYPE_2"
	default:
		return u
	}
}

// canonicalize brand กัน dropdown ซ้ำจากตัวพิมพ์ (Chargecore/ChargeCore/CHARGECORE ฯลฯ)
var brandAlias = map[string]string{
	"chargecore": "ChargeCore",
	"sungrow":    "Sungrow",
	"starcharge": "Star Charge",
	// TODO: เติมตามที่ไล่เจอตอน review §6.2
}

func canonBrand(b string) string {
	b = strings.TrimSpace(b)
	if v, ok := brandAlias[strings.ToLower(b)]; ok {
		return v
	}
	return b
}
```

**Deps:** `gorm.io/gorm/clause` (มากับ gorm อยู่แล้ว), `github.com/joho/godotenv` (ใช้อยู่แล้ว) — ไม่ต้องเพิ่ม dependency

---

## 5. Transform rules (สรุป)

| กฎ | รายละเอียด |
| --- | --- |
| ยุบ CurrentType | DC ถ้ามี connector ตัวใดเป็น DC, ไม่งั้น AC |
| ยุบ ConnectorType | หัว DC ตัวแรก (เด่นสุด) → normalize; ถ้าไม่มี DC ใช้หัวแรก |
| normalize ConnectorType | `CCS*`/`C CS2`/`CCS Combo 2` → `CCS2` · `CHAdeMO` → `CHAdeMO` · `Type 2`/`AC typr 2` → `AC_TYPE_2` |
| RatedPowerKw | `totalRatedPowerKw` → string ตรง ๆ |
| canonBrand | map ตัวพิมพ์ให้เป็นชื่อทางการ 1 แบบ |
| dedupe | `ON CONFLICT (brand, model) DO NOTHING` — แถวแรกในไฟล์ชนะ |

**Trade-off ที่ยอมรับแล้ว:** รุ่นผสม AC+DC (เช่น DELTA 3-in-1 no.125–128, EVHE104ENCA05) จะถูกยุบเป็น `DC` หัวเดียว — เสียรายละเอียดหัว AC ไป (รับได้ตาม decision model-level; ถ้าต้องการครบค่อยแตกเป็น 2 ตารางภายหลัง)

---

## 6. Data QA — ต้องทำก่อน merge

**6.1 แถวต้นทางที่คอลัมน์เพี้ยน (ตรวจตา ~5 แถว):**
- no.136 DELTRIX (คอลัมน์เลื่อน) · no.80 Chargecore (input/output voltage สลับ) · no.71 BENY (input "35") · no.272 RUIHUA (`mode` null — ไม่กระทบ เพราะไม่เก็บ mode)
- แถวเหล่านี้กระทบส่วนใหญ่ที่ field **ที่เราไม่เก็บ** → เช็คแค่ `CurrentType`/`ConnectorType`/`RatedPowerKw` พอ

**6.2 brand ซ้ำจากตัวพิมพ์ (เติม `brandAlias`):**
```sql
SELECT DISTINCT brand FROM master_chargers ORDER BY lower(brand);
```
ไล่ดูคู่ที่ต่างแค่ตัวพิมพ์ (Chargecore/CHARGECORE, Sungrow/SUNGROW, Star Charge/Starcharge, EV Plus/EVEGO ระวังอย่ารวมผิด) → เติม alias แล้ว re-run

---

## 7. ผูก `chargers` → `master_charger` (FK เฉพาะคำร้องใหม่)

hard link จาก **ตู้ที่ลงทะเบียนจริง → บัญชี master** (แนวเดียวกับ `evs.master_ev_id` ใน [`master-ev-seed-plan.md`](master-ev-seed-plan.md) §8) · **resolve ตอน create เท่านั้น, ไม่ backfill**

> **⚠️ อย่าเข้าใจผิดเรื่องคุณค่าของ FK นี้ (อ่านก่อนตัดสินใจลงแรง):**
> - **FK นี้ไม่จำเป็นต่อการทำงานของฟอร์ม** — ฟอร์มลงทะเบียน + auto-fill (§9) ทำงานได้ครบโดยไม่มีคอลัมน์นี้ มันเป็น layer สำหรับ **compliance/analytics ล้วน ๆ** (ตอบว่า "ตู้ที่ลงทะเบียนเป็น *รุ่นที่ผ่านการรับรอง* หรือไม่ / off-catalog มีกี่ตัว")
> - **ฝั่ง EV (`evs.master_ev_id`) ตอนนี้เป็น write-only** — set ตอน create ที่จุดเดียว แต่**ยังไม่มี report/admin/API ไหนอ่าน**ค่านี้เลย ถ้าไม่ทำ consumer ฝั่ง charger ก็จะ write-only เหมือนกัน
> - **Consumer (เช่น report/filter "off-catalog chargers" ในหน้า back-office) เป็น follow-up แยก** — ไม่อยู่ใน scope §7 นี้ · §7 แค่ทำให้ข้อมูลพร้อมถูก query
> - **linked rate จะสูงจริง _หลัง_ frontend เปลี่ยน brand/model เป็น dropdown เท่านั้น** (§9) — ตราบใดที่ยัง free-text อยู่ ค่า `brand/model` จะไม่ตรง canonical → `master_charger_id` ส่วนใหญ่จะ null (ไม่ใช่ bug, เป็นผลจากลำดับการ ship)
>
> **เหตุผลที่ยัง**คุ้ม**จะทำตอนนี้:** ต้นทุนต่ำมาก (nullable column + resolve ตอน create) และทำพร้อม seeder ถูกกว่ามาเขียน migration + backfill ทีหลังมาก · charger ยัง**มีเหตุผลแข็งกว่า EV** เพราะ master = "รุ่นที่ผ่านการรับรอง" การ link จึงเป็นสัญญาณ compliance ที่มีความหมายจริง

### 7.1 เพิ่มคอลัมน์บน `Charger` model
[`internal/models/registration.go`](../internal/models/registration.go) — struct `Charger`:
```go
// MasterChargerID = ลิงก์ไปบัญชี master (nullable). set ตอน create เมื่อ brand+model
// ตรงกับแถวใน master_chargers (ค่าปกติมาจาก dropdown จึงตรงเป๊ะ); null = ตู้นอกบัญชี catalog.
MasterChargerID *uint          `gorm:"column:master_charger_id;index"`
MasterCharger   *MasterCharger `gorm:"foreignKey:MasterChargerID;references:ID"`
```
`Charger` อยู่ใน `AutoMigrate` อยู่แล้ว → เพิ่มคอลัมน์อัตโนมัติตอน server boot (nullable ไม่กระทบแถวเดิม)

### 7.2 resolve ตอน create — ReService
ที่ `CreateGeneralInfoWithRelations` ลูปสร้าง `charger` ([`ReService/service.go:171`](../internal/registration/ReService/service.go)) เพิ่ม lookup ก่อน `tx.Create`:
```go
// resolve FK จาก catalog — match ด้วย (brand, model) ที่มาจาก dropdown (ตรงเป๊ะอยู่แล้ว)
var masterID *uint
var mc models.MasterCharger
if err := tx.Select("id").
    Where("brand = ? AND model = ?", charger.Brand, charger.Model).
    First(&mc).Error; err == nil {
    masterID = &mc.ID
} // ErrRecordNotFound → masterID = nil (นอกบัญชี, ยอมรับได้)
charger.MasterChargerID = masterID
```
- **join key = `(brand, model)` 2 คีย์** ตรงกับ uniqueIndex `idx_master_brand_model` (§2) — ต่างจาก EV ที่ใช้ 3 คีย์ เพราะ MasterCharger grain เป็น **ระดับรุ่น** อยู่แล้ว (§1)
- ⚠️ `charger.Brand` ต้องเป็นชื่อ **canonical เดียวกับที่ seeder เขียน** (ผ่าน `canonBrand`, §4) — ปกติมาจาก dropdown ที่ render จาก master จึงตรง; ตู้ที่พิมพ์ free-text เอง (fallback "รุ่นอื่น ๆ") จะ match ไม่ได้ → null (ยอมรับได้)
- **ไม่ backfill** แถวเก่า → เก่าคง null
- edit path (`tx.Model(...).Updates(charger)`): เซ็ต `charger.MasterChargerID` ก่อนได้เหมือนกัน — แต่ GORM `Updates(struct)` **ข้าม field ที่เป็น nil pointer**; ถ้าต้องบังคับให้กลับเป็น null ต้องเติม `.Select("master_charger_id")`

### 7.3 verify
```sql
SELECT count(*) FILTER (WHERE master_charger_id IS NOT NULL) AS linked,
       count(*) FILTER (WHERE master_charger_id IS NULL)     AS off_catalog
FROM chargers;   -- คำร้องใหม่จาก dropdown ควร linked; null = ตู้นอกบัญชี/แถวเก่า
```

---

## 8. Run & verify

```bash
# 1) ก๊อปไฟล์ข้อมูลเข้า package dir ของ seeder
cp chargers.json EVRepo/cmd/seedmaster/chargers.json

# 2) ให้ postgres รันอยู่ (docker-compose.yaml: container "postgres", DB "Test_PEA_DB", user "pea_user")
docker compose up -d postgres

# 3) รัน seeder (อ่าน DB_* จาก .env: localhost:5432)
cd EVRepo && go run ./cmd/seedmaster
```

**Verify:**
```sql
SELECT count(*) FROM master_chargers;                          -- คาด ~450–464 (หลัง dedupe)
SELECT DISTINCT brand FROM master_chargers ORDER BY brand;     -- brand ต้องไม่ซ้ำจากตัวพิมพ์
SELECT current_type, count(*) FROM master_chargers GROUP BY 1; -- ดูสัดส่วน AC/DC สมเหตุผล
SELECT DISTINCT connector_type FROM master_chargers;           -- ต้องเหลือแค่ CCS2 / CHAdeMO / AC_TYPE_2
```
รัน seeder **ซ้ำ** อีกครั้ง → `inserted 0` (ยืนยัน idempotent)

---

## 9. Downstream (PR แยก — ยังไม่อยู่ใน scope นี้)

- [x] **API:** `GET /api/v1/charger-catalog` (brand → models + spec) — ทำแล้วใน `internal/catalog` (method `GetChargerCatalog` + handler `GetChargers`, wire ที่ `cmd/server/main.go`). **public/cacheable เหมือน `/ev-catalog`** (ไม่ใส่ `CitizenAuthMiddleware`) เพื่อ consistent กับ EV catalog ที่ feed wizard เดียวกัน · **ชื่อ endpoint = `/charger-catalog`** (ไม่ใช่ `/master-chargers` ที่ร่างไว้ตอนแรก) ให้เข้าชุดกับ `/ev-catalog`
  - contract: `{ "data": [ { "brand", "models": [ { "model", "currentType", "connectorType", "totalConnectors", "ratedPowerKw" } ] } ] }`
- **Frontend:** แก้ `brand`/`model` ใน `registrationSchema.ts` จาก text input → select + auto-fill `currentType→connectorType`, `ratedPowerKw→kw`; ต้องเก็บ free-text fallback "รุ่นอื่น ๆ (ไม่มีในรายการ)" ไว้เผื่อรุ่นนอกบัญชี

---

## Deliverables checklist

- [x] `internal/models/master_charger.go` (§2)
- [x] เพิ่ม `MasterCharger` ใน `AutoMigrate` ที่ `cmd/server/main.go` (§3)
- [x] `cmd/seedmaster/main.go` + `cmd/seedmaster/chargers.json` (§4)
- [x] เติม `brandAlias` ครบหลัง QA (§6.2) — พบ `TEISON`→`Teison` เพิ่มแล้ว (คู่อื่นไม่พบ) → 98 brands, 0 case-dup
- [x] `chargers.master_charger_id` FK บน `Charger` model + resolve ตอน create ใน ReService (§7)
- [x] `go build ./...` ผ่าน + รัน seeder (464 rows) + verify + idempotent re-run (inserted 0) (§8)

> **หมายเหตุ verify (§6.1/§5 trade-off):** `connector_type` ที่ seed จริงเหลือแค่ `CCS2` / `AC_TYPE_2` — **ไม่มี `CHAdeMO`** เพราะมี CHAdeMO แค่ 3 รุ่น (DELTA) และทุกตัว connector ตัวแรกเป็น `CCS 2` ก่อน `CHAdeMO` → กฎ collapse "หัว DC ตัวแรกเด่นสุด" เลือก CCS2 (เป็นไปตาม trade-off model-level ที่ยอมรับแล้ว ไม่ใช่ bug)
