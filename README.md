# EVRepo — PEA Watt-D · EV Wall Box Registration (Backend)

Backend ของระบบลงทะเบียน **เครื่องชาร์จ (Wall Box) + รถ EV** ผูกกับเลข **CA** (บัญชีผู้ใช้ไฟ)
ของการไฟฟ้าส่วนภูมิภาค (กฟภ.) — รองรับทั้งฝั่งประชาชน (ThaID) และฝั่งเจ้าหน้าที่ (Keycloak / back-office)

Frontend อยู่คนละโปรเจกต์: `../ev-registration` (Next.js) ซึ่ง proxy `/api/*` มาที่ backend นี้

**Stack:** Go 1.24 · Gin · GORM · PostgreSQL 15 · S3-compatible storage (Dell EMC ECS) · Keycloak + ThaID OAuth

---

## 1. Setup

ต้องมี **Go 1.24+** และ **Docker** (สำหรับ Postgres)

```bash
# 1. ยก Postgres ขึ้น
docker compose up -d postgres

# 2. รัน server (AutoMigrate ทุกตารางให้อัตโนมัติตอน start)
go run ./cmd/server
```

> 📌 **`.env` ไม่ได้อยู่ใน git** (อยู่ใน `.gitignore`) และรีโปนี้ **ไม่มี `.env.example`** —
> ต้องขอไฟล์จากทีม หรือสร้างเองตามรายการตัวแปรใน §5 ก่อนรันครั้งแรก

server ฟังที่ `PORT` (`.env` ปัจจุบัน = **8080**) · ถ้าไม่ตั้ง `PORT` โค้ด fallback เป็น `3000`

รันทั้ง stack (backend + postgres) ด้วย Docker:

```bash
docker compose up -d
```

---

## 2. Seed & Tools

ทุกตัวอ่าน `.env` เอง (`godotenv`) และ **idempotent** — รันซ้ำได้ปลอดภัย

| คำสั่ง | ทำอะไร | Idempotency |
| --- | --- | --- |
| `go run ./cmd/seedcampaign` | ช่วงเวลากิจกรรม (campaign window) 1 แถว active สำหรับ dev/test | ข้ามถ้ามี campaign active อยู่แล้ว |
| `go run ./cmd/seedmaster` | บัญชีรุ่น **เครื่องชาร์จ** ที่ผ่านการรับรอง (`MasterCharger`) จาก `chargers.json` | `ON CONFLICT (brand, model) DO NOTHING` |
| `go run ./cmd/seedevmaster` | บัญชีรุ่น **รถ EV** (`MasterEV`) จาก `ev_master_seed.json` | `ON CONFLICT (brand, model, battery_label) DO NOTHING` |

**ข้อมูลตัวอย่าง (vendor / general_info / charger / ev)** สำหรับเทสต์ อยู่ใน `scripts/seed.sql`:

```bash
docker exec -i postgres psql -U pea_user -d Test_PEA_DB < scripts/seed.sql
```

> ⚠️ header ใน `scripts/seed.sql` เขียน container ว่า `postgres-pea` แต่ `docker-compose.yaml`
> ตั้ง `container_name: postgres` — ใช้ชื่อตาม compose (`postgres`) ตามคำสั่งข้างบน

### เครื่องมือช่วยเทสต์

| คำสั่ง | ทำอะไร |
| --- | --- |
| `go run ./cmd/mockcitizen -pid 1234567890123 -first-name ทดสอบ -last-name ระบบ -entry-source smartplus` | พิมพ์ token `citizen_session` ที่ใช้ได้จริง — เทสต์ endpoint ฝั่งประชาชนโดยไม่ต้อง login ThaID จริง |
| `go run ./cmd/s3test` | smoke test ค่า S3 ใน `.env` — upload → presign → fetch กลับ |

ตัวอย่างใช้ token จาก `mockcitizen`:

```bash
curl -X POST http://localhost:8080/api/v1/general-info \
  --cookie "citizen_session=<printed token>" \
  -F 'data={"ca":"123456789012","chargers":[],"evs":[]}'
```

---

## 3. API Surface

### Public (ไม่ต้อง login)

| Method | Path | หมายเหตุ |
| --- | --- | --- |
| GET | `/api/v1/ev-catalog` | master catalog รถ EV — feed dropdown ของ wizard |
| GET | `/api/v1/charger-catalog` | master catalog เครื่องชาร์จ + auto-fill spec |
| GET | `/api/v1/campaign` | สถานะช่วงเวลากิจกรรม (`before` / `open` / `closed`) — คำนวณจาก **server clock** |

### Citizen (ThaID — cookie `citizen_session`)

| Method | Path | หมายเหตุ |
| --- | --- | --- |
| GET | `/api/v1/general-info/check-ca?ca=...` | lookup CA (เช็ค DB เราก่อน แล้วค่อยถาม cs-service ของ กฟภ.) — read-only |
| POST | `/api/v1/general-info` | **จุด submit จริง** (multipart + รูป) — มี campaign submit gate |
| GET | `/api/v1/general-info/me` | คำขอของตัวเอง |

### Staff / Back-office (Keycloak — cookie `access_token`)

| Method | Path |
| --- | --- |
| GET | `/api/v1/general-info` |
| GET | `/api/v1/admin/me` · `/stats` · `/registrations` · `/registrations/:id` |
| PATCH | `/api/v1/admin/registrations/:id` · `/:id/checklist` · `/:id/notes` |
| POST | `/api/v1/admin/registrations/:id/decision` |
| GET / PATCH | `/api/v1/admin/campaign` — ตั้งค่าช่วงเวลากิจกรรม |

### OAuth (full-page redirect — **อยู่นอก `/api` โดยตั้งใจ**)

`GET /login` · `/dashboard` (callback) · `/logout` — Keycloak
`GET /thaid/login` · `/thaid/callback` · `/thaid/logout` — ThaID/DOPA

> route เหล่านี้ต้องเป็น browser redirect ไม่ใช่ fetch/XHR จึงไม่ผ่าน rewrite proxy `/api/*` ของ frontend

---

## 4. Layout

```
cmd/
  server/         entrypoint — wiring + routes ทั้งหมด
  seedcampaign/   seed ช่วงเวลากิจกรรม
  seedmaster/     seed master เครื่องชาร์จ (chargers.json)
  seedevmaster/   seed master รถ EV (ev_master_seed.json)
  mockcitizen/    ออก citizen_session token สำหรับเทสต์
  s3test/         smoke test S3
internal/
  models/         GORM models (AutoMigrate ที่ cmd/server/main.go)
  campaign/       ช่วงเวลากิจกรรม — service + handler (public + admin)
  registration/   ReService (business logic) + controller (HTTP)
  admin/          back-office review console — handler/service/model (flag + points engine, audit)
  auth/           Keycloak + ThaID OAuth (config/handler/service)
  catalog/        master EV/charger catalog (read-only)
  middleware/     AuthMiddleware (Keycloak) · CitizenAuthMiddleware (ThaID)
  peacs/          client ของ cs-service (ฐานข้อมูลลูกค้าจริง กฟภ.)
  storage/        S3 upload + presign
  database/       connection
scripts/seed.sql  ข้อมูลตัวอย่างสำหรับเทสต์
```

---

## 5. Environment Variables

ตั้งใน `.env` (โหลดด้วย `godotenv` ทั้ง server และ seeder ทุกตัว)

| กลุ่ม | ตัวแปร |
| --- | --- |
| App | `ENV` · `PORT` · `FRONTEND_URL` |
| Database | `DB_HOST` · `DB_PORT` · `DB_USER` · `DB_PASSWORD` · `DB_NAME` |
| Keycloak | `KEYCLOAK_CLIENT_ID` · `KEYCLOAK_CLIENT_SECRET` · `KEYCLOAK_ISSUER` · `KEYCLOAK_PUBLIC_KEY` · `KEYCLOAK_LOGIN_URL` · `KEYCLOAK_TOKEN_URL` · `KEYCLOAK_USERINFO_URL` · `KEYCLOAK_LOGOUT_URL` · `KEYCLOAK_REDIRECT_URI` |
| ThaID | `THAID_CLIENT_ID` · `THAID_CLIENT_SECRET` · `THAID_AUTH_URL` · `THAID_TOKEN_URL` · `THAID_USERINFO_URL` · `THAID_REVOKE_URL` · `THAID_REDIRECT_URI` |
| Citizen session | `CITIZEN_SESSION_SECRET` |
| S3 | `AWS_ENDPOINT_URL` · `AWS_REGION` · `AWS_ACCESS_KEY_ID` · `AWS_SECRET_ACCESS_KEY` · `S3_BUCKET_NAME` |
| PEA cs-service | `PEA_CS_SERVICE_URL` |

`KEYCLOAK_PUBLIC_KEY` **บังคับ** — ไม่มีแล้ว server `log.Fatal` ตอน start

---

## 6. Conventions

- **วันที่เก็บเป็น UTC เสมอ** — แปลง/แสดงเป็นเวลาไทย (พ.ศ.) ที่ frontend
- **PID มาจาก ThaID/DOPA ทางเดียว** — ไม่เคย trust จาก request body (`CitizenAuthMiddleware` → `ctx.MustGet("citizen")`)
- **business rule อยู่ที่ backend เสมอ** — frontend คำนวณคู่ขนานได้แค่เพื่อ UX, การอนุมัติ/การ gate ตัดสินที่นี่เท่านั้น
  (เช่น campaign submit gate ใน `registration/ReService`, flag/points engine ใน `admin/service`)
- **AuditLog** ผูกกับ `GeneralInfoID` (not null) — เป็น trail ของคำขอ ไม่ใช่ของ config ระดับระบบ
