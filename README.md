# EVRepo — EV-Voluntary-Registration-Form (Backend · PEA Watt-D)

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

server ฟังที่ `PORT` (`.env` ปัจจุบัน = **8080**) · ถ้าไม่ตั้ง `PORT` โค้ด fallback เป็น **`8080`** เหมือนกัน
— **backend = 8080 · frontend = 3000** เป็นค่ามาตรฐานของโปรเจกต์นี้ (fallback เดิมเป็น `3000` ซึ่งชนกับ `next dev`)

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
| `go run ./cmd/seedrbac` | RBAC ฝั่ง back-office — `MasterRole` / `RulePolicy` / `MasterUser` (allow-list ตาม `dept_change_code`) | upsert ตาม unique key · `MasterUser` upsert บน `employee_id` |

รันครบทั้ง 4 ตัวรวดเดียวด้วย `./scripts/seed-dev.sh` (หรือ `.\scripts\seed-dev.ps1` บน Windows) ·
เติม `--demo` / `-Demo` เพื่อ seed คำขอตัวอย่างสำหรับหน้า dashboard (`cmd/seeddashboarddemo`)

> ⚠️ **ไม่ seed = ระบบใช้ไม่ได้** — ไม่มี `seedrbac` เจ้าหน้าที่ login ผ่านแต่ทุก `/api/v1/admin/*`
> ตอบ 403 `NO_ROLE` · ไม่มี `seedcampaign` ระบบ **fail closed** (ไม่มี campaign active = `closed`)
> ประชาชน submit ไม่ได้ · ไม่มี master catalog dropdown ใน wizard ว่าง

#### เพิ่มสิทธิ์หน่วยงานโดยไม่ต้องแก้โค้ด

`deptRoleMap` ใน `cmd/seedrbac/main.go` เป็นแค่ **ค่า default** — ถ้าคนที่จะเทสต์อยู่หน่วยงานอื่น
เพิ่มได้ 2 ทางโดยไม่ต้อง rebuild:

```bash
# ก) ทั้งหน่วยงาน (รับพนักงานจาก employees.csv ที่ dept ตรงกันมาด้วย)
RBAC_DEPT_ROLES="530203003000000:operator,210102006000000:executive" go run ./cmd/seedrbac

# ข) รายคน — สำหรับ dept ที่ไม่มีใน employees.csv (ต้องครบทั้ง 4 flag)
go run ./cmd/seedrbac -dept 530203003000000 -role operator -emp 515731 -name "ชื่อ นามสกุล"
```

`role` รับแค่ `operator` / `executive` — พิมพ์ผิดจะ error ทันทีก่อนแตะ DB

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

### OAuth — **อยู่ใต้ `/api/auth` ทั้งหมด**

| Endpoint | ชนิด | หมายเหตุ |
| --- | --- | --- |
| `GET /api/auth/login` | full-page redirect | Keycloak (พนักงาน) |
| `GET /api/auth/callback` | full-page redirect | Keycloak callback — เดิมชื่อ `/dashboard` |
| `GET /api/auth/logout` | **XHR** ตอบ JSON | เรียกจาก `services/auth.service.ts` |
| `GET /api/auth/thaid/login` | full-page redirect | ThaID/DOPA (ประชาชน) |
| `GET /api/auth/thaid/callback` | full-page redirect | ThaID callback |
| `GET /api/auth/thaid/logout` | **XHR** ตอบ JSON | เรียกจาก `services/auth.service.ts` |
| `GET /dashboard` | ⏳ **alias ชั่วคราว** | ชี้ handler เดียวกับ `/api/auth/callback` — มีไว้เพราะ client ฝั่ง Keycloak ยังลงทะเบียน URI เดิม · ลบพร้อมตอนแก้ `KEYCLOAK_REDIRECT_URI` |

> เดิมอยู่ที่ราก (`/login`, `/dashboard`, `/logout`, `/thaid/*`) ซึ่งบังคับให้ ingress ต้องประกาศ
> path แยก **5 กลุ่ม** — ลืมกลุ่มไหน route นั้นจะตกไปที่ catch-all ของ Next แล้วตอบ 404 เงียบ ๆ
> โดยไม่มี log ฝั่งไหนชี้เลย · ย้ายมาใต้ `/api` แล้ว ingress เหลือ **path เดียว**
>
> ⚠️ group `/api/auth` **ห้ามแขวน `AuthMiddleware`** — มันคือ route ที่ใช้ login เอง

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
  admin/          back-office review console — handler/service/model (list/paging + claim-queue + inline-edit/decision + audit · ตรวจ/อนุมัติ-ปฏิเสธเท่านั้น ไม่มี flag/points engine)
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
| App | `ENV` · `PORT` · `FRONTEND_URL` · `COOKIE_SECURE` (optional) |
| Database | `DB_HOST` · `DB_PORT` · `DB_USER` · `DB_PASSWORD` · `DB_NAME` |
| Keycloak | `KEYCLOAK_CLIENT_ID` · `KEYCLOAK_CLIENT_SECRET` · `KEYCLOAK_ISSUER` · `KEYCLOAK_PUBLIC_KEY` · `KEYCLOAK_LOGIN_URL` · `KEYCLOAK_TOKEN_URL` · `KEYCLOAK_USERINFO_URL` · `KEYCLOAK_LOGOUT_URL` · `KEYCLOAK_REDIRECT_URI` |
| ThaID | `THAID_CLIENT_ID` · `THAID_CLIENT_SECRET` · `THAID_AUTH_URL` · `THAID_TOKEN_URL` · `THAID_USERINFO_URL` · `THAID_REVOKE_URL` · `THAID_REDIRECT_URI` |
| Citizen session | `CITIZEN_SESSION_SECRET` |
| S3 | `AWS_ENDPOINT_URL` · `AWS_REGION` · `AWS_ACCESS_KEY_ID` · `AWS_SECRET_ACCESS_KEY` · `S3_BUCKET_NAME` |
| PEA cs-service | `PEA_CS_SERVICE_URL` |

`KEYCLOAK_PUBLIC_KEY` **บังคับ** — ไม่มีแล้ว server `log.Fatal` ตอน start

`COOKIE_SECURE` (`true`/`false`) override flag `Secure` ของ auth cookie ทุกใบ — ปกติ flag นี้ตามชื่อ
stage (`ENV=production`) แต่ตัวที่ควรตัดสินจริงคือ **scheme ที่ deploy ให้บริการ** ไม่ใช่ชื่อ stage:
dev/staging ที่อยู่หลัง HTTPS ต้อง `COOKIE_SECURE=true` (ไม่งั้น cookie รั่วถ้ามี request http
ไปโฮสต์เดียวกัน และถูก MITM เขียนทับได้) · ส่วน production build ที่รันโลคอลผ่าน http ต้อง
`COOKIE_SECURE=false` ไม่งั้น browser ทิ้ง cookie แล้ว login ไม่ติด · ไม่ตั้ง = ตาม `ENV` เหมือนเดิม

| `ENV` | `COOKIE_SECURE` | ผลลัพธ์ |
| --- | --- | --- |
| `development` | ไม่ตั้ง | `Secure=false` — dev โลคอล http |
| `development` | `true` | `Secure=true` — **dev/staging หลัง HTTPS ใช้อันนี้** |
| `production` | ไม่ตั้ง | `Secure=true` |
| `production` | `false` | `Secure=false` — production build ที่รันโลคอล http |

> ยังเหลือ hardening อีกชุด (SameSite ยังไม่ตั้งชัด, ชื่อ cookie ThaID ชนกับ Keycloak,
> path scope ของ staff cookie) — ดู `internal/auth/service/cookie.go` และ handler ที่เรียก `SecureCookie()`

---

## 6. Conventions

- **วันที่เก็บเป็น UTC เสมอ** — แปลง/แสดงเป็นเวลาไทย (พ.ศ.) ที่ frontend
- **PID มาจาก ThaID/DOPA ทางเดียว** — ไม่เคย trust จาก request body (`CitizenAuthMiddleware` → `ctx.MustGet("citizen")`)
- **business rule อยู่ที่ backend เสมอ** — frontend คำนวณคู่ขนานได้แค่เพื่อ UX, การอนุมัติ/การ gate ตัดสินที่นี่เท่านั้น
  (เช่น campaign submit gate ใน `registration/ReService`, decision/transition guard ใน `admin/service`) ·
  back-office ตรวจ/อนุมัติ-ปฏิเสธเท่านั้น — **ไม่คำนวณ flag/มอบ Watt-D Points** (ตัดออกแล้ว มติ 2026-07-22)
- **AuditLog** ผูกกับ `GeneralInfoID` (not null) — เป็น trail ของคำขอ ไม่ใช่ของ config ระดับระบบ
