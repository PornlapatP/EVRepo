# Deploy — EV-Voluntary-Registration-Form (backend / Go)

> **สถานะ:** เขียนจาก `values.dev.yml` ตัวจริงที่ DevSecOps ให้มา (2026-07-31) เทียบกับโค้ดใน repo นี้
> deployment path = **GitLab CI → Harbor → Argo CD → k8s** เหมือนฝั่ง frontend
>
> ⚠️ ส่วนที่ทำเครื่องหมาย 🔴 คือ **ของที่ต้องแก้ก่อน deploy** ถ้าปล่อยตามไฟล์ตัวอย่างจะพัง
> ❓ = ยังไม่ได้คำตอบจาก DevSecOps

---

## 1. 🔴 กับดัก 3 อันใน `values.dev.yml` ตัวอย่าง — ต้องแก้ก่อน

### D1 — `path: /` ชนกับ catch-all ของ frontend

ไฟล์ตัวอย่างของ backend เขียนไว้ว่า:

```yaml
ingress:
  enabled: true
  hosts:
    - host: evregist-dev.com
      paths:
        - path: /            # ← Prefix catch-all
          serviceName: evregist-be
```

แต่ฝั่ง frontend ก็จะประกาศ `host: evregist-dev.com` + `path: /` เหมือนกัน
→ **Ingress 2 ตัวแย่ง host+path เดียวกันเป๊ะ** ซึ่ง nginx ingress ไม่ merge ให้
มันเลือกตัวที่ `creationTimestamp` เก่ากว่า → **ทั้งเว็บจะวิ่งเข้า service เดียวแบบสุ่ม**
(ถ้า BE ชนะ = ผู้ใช้เห็น JSON 404 ของ Gin แทนหน้าเว็บ · ถ้า FE ชนะ = `/api/*` ตาย)

**ตัดสินใจแล้ว (2026-07-31): เปิด Ingress ทั้ง 2 ฝั่ง แล้วแบ่ง path กัน**
nginx-ingress รวบทุก Ingress ที่ `host` เดียวกันเข้า server block เดียว แล้วเอาแต่ละ path
ไปเป็น location — **จะชนก็ต่อเมื่อ host+path ซ้ำกันเป๊ะ** พอ BE ถือ path เฉพาะของตัวเอง
และ FE ถือ `/` เป็น catch-all จึงไม่ทับกัน (ค่าที่ต้องแก้ = **`path`** ไม่ใช่ `enabled`)

```yaml
ingress:
  enabled: true                                       # ✅ คงไว้
  hosts:
    - host: <hostname จริง>                           # ← ต้องเป็น string เดียวกันเป๊ะกับฝั่ง FE
      paths:                                          # 🔴 เปลี่ยนจาก `/` เป็น 5 กลุ่มนี้ (ดู §3)
        - { path: /api,       pathType: Prefix, serviceName: evregist-be, servicePort: http }
        - { path: /thaid,     pathType: Prefix, serviceName: evregist-be, servicePort: http }
        - { path: /login,     pathType: Exact,  serviceName: evregist-be, servicePort: http }
        - { path: /logout,    pathType: Exact,  serviceName: evregist-be, servicePort: http }
        - { path: /dashboard, pathType: Exact,  serviceName: evregist-be, servicePort: http }
```

> 🔴 **`/api` อย่างเดียวไม่พอ** — backend มี route ที่อยู่ **นอก `/api`** โดยตั้งใจ เพราะเป็น
> full-page OAuth redirect ไม่ใช่ fetch ([`main.go:161-175`](cmd/server/main.go#L161-L175))
> ถ้าไม่ประกาศครบ 5 กลุ่ม path ที่ขาดจะตกไปที่ catch-all `/` ของ FE แล้ว **Next.js ตอบ 404**
> → ประชาชน login ThaID ไม่ได้ · พนักงาน login Keycloak ไม่ได้ · อาการเหมือน
> "หน้าเว็บขึ้นปกติแต่กดปุ่มแล้ว 404" ซึ่ง debug ยากมาก

**กติกาที่ต้องรักษาเมื่อใช้ 2 Ingress:**

| # | กติกา | ถ้าลืม |
| --- | --- | --- |
| 1 | **เพิ่ม route นอก `/api` เมื่อไหร่ ต้องเพิ่ม path ใน values ของ BE ด้วยเสมอ** | route ใหม่ตกไป FE → **404 เงียบ ๆ** ไม่มี error ให้เห็น |
| 2 | `host` ต้องเป็น string เดียวกันเป๊ะทั้ง 2 ไฟล์ | กลายเป็นคนละ server block → path ของอีกฝั่งหาย |
| 3 | **`tls:` ประกาศฝั่งเดียว** (แนะนำที่ FE เพราะเป็นเจ้าของ `/`) | nginx เลือก cert ให้ host นั้นได้ใบเดียว — ประกาศ 2 ที่ด้วยคนละ secret จะได้ใบที่ไม่ได้ตั้งใจ |
| 4 | ทั้ง 2 Ingress อยู่ namespace เดียวกัน | nginx เตือน host conflict ข้าม namespace |

> `pathType: Prefix` ของ k8s match เป็น **segment** — `/api` จับ `/api/v1/campaign` แต่ **ไม่จับ** `/apifoo` ✅
> ส่วน `/*` (redirect_uri ที่ DOPA ล็อกไว้) ตกที่ FE ตามตั้งใจ เพราะ `proxy.ts` ดักไว้เอง

### D2 — `containerPort` ต้องเป็น `8080` (ตัวอย่างเขียน `3000`)

**ตัดสินใจแล้ว (2026-07-31): backend = `8080` · frontend = `3000`** — เดินทาง B (B6) เรียบร้อย

| ที่ | ค่าที่ถูก |
| --- | --- |
| [`main.go:187-193`](cmd/server/main.go#L187-L193) | `PORT` ถ้าไม่ตั้ง → default **`8080`** ✅ แก้แล้ว |
| [`Dockerfile:19`](Dockerfile#L19) | `EXPOSE 8080` ✅ ตรงอยู่แล้ว |
| `values.dev.yml` | 🔴 ตัวอย่างเป็น `containerPort: 3000` · `targetPort: 3000` — **ต้องแก้เป็น `8080` ทั้งคู่** |

เดิม default ของ Go เป็น `3000` ซึ่ง **ชนกับ `next dev` ของ frontend** ตอนรัน local โดยไม่ตั้ง `PORT`
และไม่ตรงกับ `EXPOSE 8080` · ตอนนี้ทั้งโค้ด/Dockerfile/เอกสารพูดตรงกันหมดที่ `8080` แล้ว

> 🔴 **`values.dev.yml` เป็นที่เดียวที่ยังค้าง** — ถ้าปล่อย `containerPort`/`targetPort` เป็น `3000`
> ไว้ Service จะชี้ไปพอร์ตที่ไม่มีใครฟัง → **502 ทุก request** · แก้ทั้ง 2 ค่าในคอมมิตเดียวกัน
> (ถ้าอยากคุมแบบ explicit ตั้ง `PORT: '8080'` ใน `env:` ไปด้วย — ค่าเดียวกับ default อยู่แล้ว)

### D3 — `envFrom` ชี้ secret เดียว แต่ backend ต้องการ env 33 ตัว

ไฟล์ตัวอย่างมี `env:` แค่ `APP_VERSION` และดึงที่เหลือทั้งหมดจาก
`envFrom: evregist-be-secret` (Vault path `dev/api/evregist-be-secret`)

**ปัญหา:** backend อ่าน env **33 ตัว** แต่มีแค่ **6 ตัวที่เป็น secret จริง** — ถ้ายัดทั้ง 33 ตัวลง Vault
จะกลายเป็นว่าทุกครั้งที่แก้ config ธรรมดา (เช่น `FRONTEND_URL`) ต้องขอสิทธิ์เขียน Vault

**ที่แนะนำ — แยก 2 ชั้น:**

```yaml
  envFrom:
    - name: evregist-be-secret      # เฉพาะ 6 ตัวที่เป็น secret จริง
      type: secret
  env:
    APP_VERSION:      { value: '' }
    ENV:              { value: 'production' }   # 🔴 ต้องเป็น production
    COOKIE_SECURE:    { value: 'true' }         # 🔴 ต้องเป็น true (HTTPS)
    PORT:             { value: '8080' }         # ให้ตรง D2 (= default ของโค้ด)
    FRONTEND_URL:     { value: 'https://<hostname จริง>' }
    DB_HOST:          { value: '<db host>' }
    # … ที่เหลือดู §2
```

> 🔴 **backend `log.Fatal` ทันทีถ้า env ขาด** → **CrashLoopBackOff** ไม่ใช่ error เงียบ ๆ:
> - [`main.go:33-36`](cmd/server/main.go#L33-L36) `KEYCLOAK_PUBLIC_KEY` ว่าง → `log.Fatal`
> - [`main.go:50-53`](cmd/server/main.go#L50-L53) `storage.New` fail (ไม่มี `S3_BUCKET_NAME`) → `log.Fatal`
> - `database.Connect()` ต่อ DB ไม่ได้ → panic/fatal
>
> ดังนั้น **env ไม่ครบ = pod ไม่ขึ้นเลย** ไม่ใช่ขึ้นแล้วค่อยเจอปัญหาทีหลัง

---

## 2. Environment Variable ทั้ง 33 ตัว

### 🔒 Secret (6 ตัว) → Vault `evregist` / `dev/api/evregist-be-secret`

| Variable | ใช้ทำอะไร |
| --- | --- |
| `KEYCLOAK_CLIENT_SECRET` | OAuth พนักงาน |
| `THAID_CLIENT_SECRET` | OAuth ประชาชน (DOPA) |
| `DB_PASSWORD` | Postgres |
| `CITIZEN_SESSION_SECRET` | เซ็น `citizen_session` cookie |
| `AWS_ACCESS_KEY_ID` | S3 (geodrive) — SDK default credential chain |
| `AWS_SECRET_ACCESS_KEY` | S3 (geodrive) — SDK default credential chain |

### ⚙️ Config (25 ตัว) → `deployment.env` ธรรมดา

| กลุ่ม | Variable |
| --- | --- |
| Keycloak | `KEYCLOAK_LOGIN_URL` · `KEYCLOAK_TOKEN_URL` · `KEYCLOAK_USERINFO_URL` · `KEYCLOAK_LOGOUT_URL` · `KEYCLOAK_ISSUER` · `KEYCLOAK_PUBLIC_KEY` · `KEYCLOAK_CLIENT_ID` · `KEYCLOAK_REDIRECT_URI` |
| ThaID / DOPA | `THAID_AUTH_URL` · `THAID_TOKEN_URL` · `THAID_USERINFO_URL` · `THAID_REVOKE_URL` · `THAID_CLIENT_ID` · `THAID_REDIRECT_URI` |
| Database | `DB_HOST` · `DB_PORT` · `DB_USER` · `DB_NAME` |
| S3 / geodrive | `AWS_REGION` · `AWS_ENDPOINT_URL` · `S3_BUCKET_NAME` |
| อื่น ๆ | `FRONTEND_URL` · `PEA_CS_SERVICE_URL` · `RBAC_DEPT_ROLES` · `PORT` |

> `KEYCLOAK_PUBLIC_KEY` เป็น **public** key (ไม่ใช่ secret) แต่เป็น blob ยาว — ใส่ใน ConfigMap/`env` ได้
> `AWS_REGION`/`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` ไม่ได้ถูกอ่านด้วย `os.Getenv` ตรง ๆ
> แต่ AWS SDK อ่านเองผ่าน default credential chain ([`internal/storage`](internal/storage))

### 🚩 2 ตัวที่พลาดบ่อย

| Variable | ต้องเป็น | ถ้าผิด |
| --- | --- | --- |
| `ENV` | `production` | cookie ไม่ถูกตั้งโหมด production |
| `COOKIE_SECURE` | `true` | cookie ไม่มี `Secure` → บน HTTPS browser ยังส่ง แต่เสี่ยง downgrade · **ถ้า ingress เป็น HTTP ล้วนแล้วตั้ง `true` cookie จะไม่ถูกส่งกลับเลย → login พังทั้งระบบ** (ผูกกับ ❓ TLS §5) |

### 🔴 URL 3 ตัวที่ต้องเปลี่ยนเป็น hostname จริง (ไม่ใช่ localhost)

`KEYCLOAK_REDIRECT_URI` · `THAID_REDIRECT_URI` · `FRONTEND_URL`
— และ **2 ตัวแรกต้องถูกลงทะเบียนฝั่ง DOPA/Keycloak ด้วย** ไม่งั้น OAuth ตีกลับ (ดู §6)

---

## 3. Routing — backend รับ path ไหนบ้าง

**host เดียว + แบ่ง path ระหว่าง 2 Ingress** — BE ประกาศ path ของตัวเอง, FE ถือ `/` เป็น catch-all

```
                    host เดียว (server block เดียวใน nginx)
   ┌──────────────────────────────┴──────────────────────────────┐
   │ Ingress: evregist-be          │  Ingress: evregist-fe       │
   │  /api  /thaid                 │   /  (catch-all)            │
   │  /login  /logout  /dashboard  │                             │
   └───────────────┬───────────────┴──────────────┬──────────────┘
                   ▼                              ▼
            svc/evregist-be :80            svc/evregist-fe :80
```

ที่มาของแต่ละ path — [`cmd/server/main.go:90-185`](cmd/server/main.go#L90-L185):

| Path | Route ในโค้ด |
| --- | --- |
| `/api/v1/*` | `apiV1 := r.Group("/api/v1")` — catalog, campaign, general-info, admin |
| `/api/profile` · `/api/thaid/profile` | `main.go:177-185` |
| `/thaid/login` · `/thaid/callback` · `/thaid/logout` | `main.go:170-175` |
| `/login` · `/logout` | Keycloak — `main.go:162-167` |
| `/dashboard` | 🔴 **Keycloak callback** — `main.go:165` |

> 🔴 **`/dashboard` เป็นของ backend** — ถ้าฝั่ง frontend เผลอสร้าง `app/dashboard/page.tsx`
> เมื่อไหร่ login พนักงานจะพังแบบ debug ยากมาก (คอมเมนต์เตือนไว้แล้วทั้ง 2 ฝั่ง)

**ผลพลอยได้ — CORS หายไปเอง:** พอ FE/BE อยู่ host เดียวกัน request เป็น **same-origin**
browser ไม่ยิง preflight เลย → CORS ที่ hardcode `localhost:3000/3001` ไว้ที่
[`main.go:60-76`](cmd/server/main.go#L60-L76) จึงไม่ทำให้พังบน k8s
แต่ **B4 ยังควรทำ** เพราะค่านั้นบังคับให้ dev ทุกคนต้องใช้พอร์ตตายตัว และถ้าอนาคตมี origin อื่นจะพังทันที

---

## 4. สิ่งที่ต้องแก้ในโค้ด/Dockerfile ก่อน deploy (B1–B8)

> ยังไม่ได้ทำ — นี่คือรายการจาก MIGRATION-PLAN §5.3

| # | ระดับ | ปัญหา | ต้องทำ |
| --- | --- | --- | --- |
| **B1** | 🔴 | `go.mod` = `go 1.25.0` แต่ [`Dockerfile:2,14`](Dockerfile#L2) ใช้ `golang:1.24` → **build fail** | ยกเป็น `golang:1.25` (หรือ base ที่ DevSecOps ให้) |
| **B2** | 🔴 | runtime stage เป็น `golang` เต็มตัว (~1.2GB มี compiler+shell) และรันเป็น **root** → Trivy CVE เพียบ + ไม่ผ่าน policy non-root | runtime เป็น distroless/alpine + non-root uid `65532`<br>⚠️ **ต้องมี `ca-certificates`** — เรียก HTTPS ไป DOPA/Keycloak/geodrive ถ้าใช้ `scratch` เปล่า TLS พังทั้งระบบ |
| **B3** | 🟠 | ไม่มี health endpoint | เพิ่ม `GET /healthz` + `GET /readyz` (ping DB) **นอก** auth middleware<br>🟢 **ลดความเร่งด่วนแล้ว** — values ตั้ง `livenessProbe.enabled: false` และ startup/readiness ยัง comment ไว้ → ถ้าไม่เปิด probe เลย B3 ไม่ใช่ blocker |
| **B4** | 🟠 | CORS hardcode `localhost:3000/3001` + `AllowCredentials: true` | อ่าน origin จาก env (`FRONTEND_URL` มีอยู่แล้วแต่ยังไม่ได้ใช้ตรงนี้) รับ comma-separated<br>🟢 ไม่ blocker บน k8s เพราะ same-origin (§3) |
| **B5** | 🟠 | `AutoMigrate` ตอน boot ([`main.go:32`](cmd/server/main.go#L32)) → หลาย replica migrate ชนกัน | 🟢 **values ตั้ง `replicas: 1` แล้ว ปลอดภัย** — 🔴 **ห้ามขึ้น replicas จนกว่าจะย้าย migrate ไป Argo PreSync hook Job** |
| **B6** | ✅ | `PORT` default = `3000` ไม่ตรง `EXPOSE 8080` (และชนกับ `next dev`) | แก้ default เป็น `8080` แล้ว — เหลือ `values.dev.yml` ที่ยังต้องเปลี่ยน `containerPort`/`targetPort` เป็น `8080` (ดู **D2**) |
| **B7** | 🟡 | `ENV`/`COOKIE_SECURE` | ตั้งใน `deployment.env` (§2) |
| **B8** | 🟡 | `godotenv.Load()` ตอน boot ([`main.go:28`](cmd/server/main.go#L28)) | ไม่ต้องแก้ (error ถูก ignore) แต่ต้องมั่นใจว่า env มาจาก Vault/env ครบ |

---

## 5. values.dev.yml ที่แก้แล้ว (ร่าง)

```yaml
applicationName: "evregist-be"

### DEPLOYMENT ###
deployment:
  enabled: true
  replicas: 1                        # 🔴 ห้ามขึ้น — AutoMigrate ตอน boot (B5)
  imagePullSecrets: "harbor-regcred"
  image:
    repository: "harbor.<domain>/evregist/web/evregist-be"   # ❓ "web/" สำหรับ backend — ยืนยันกับ DevSecOps
    tag: ''
    digest: ''
    pullPolicy: IfNotPresent

  # Probe — liveness ปิดตามมาตรฐานทีม (มติ 2026-07-31)
  livenessProbe:
    enabled: false
  # readinessProbe:                  # ถ้าจะเปิด ต้องทำ B3 (/readyz) ก่อน
  #   enabled: true
  #   httpGet:
  #     path: /readyz
  #     port: 8080                   # 🔴 containerPort ไม่ใช่ 80 (ตัวอย่างเขียน 80 = service port ผิด)

  resources:
    limits:
      memory: 512Mi
    requests:                        # 🔴 ตัวอย่างไม่มี requests → scheduler จองทรัพยากรผิด
      memory: 256Mi

  ports:
    - containerPort: 8080            # ต้องตรงกับ PORT (ดู D2) — ตัวอย่างเขียน 3000 = ผิด
      name: http
      protocol: TCP

  envFrom:
    - name: evregist-be-secret       # 6 secret จาก Vault
      type: secret

  env:                               # 🔴 config ที่เหลือ — ไม่ควรยัดลง Vault
    APP_VERSION:       { value: '' }
    ENV:               { value: 'production' }
    COOKIE_SECURE:     { value: 'true' }
    PORT:              { value: '8080' }
    FRONTEND_URL:      { value: 'https://<hostname จริง>' }
    # … Keycloak 8 · ThaID 6 · DB 4 · S3 3 · PEA_CS_SERVICE_URL · RBAC_DEPT_ROLES (ดู §2)

### SERVICE ###
service:
  enabled: true
  type: ClusterIP
  ports:
    - port: 80                       # in-cluster เรียกที่ 80 → FE ตั้ง API_PROXY_TARGET=http://evregist-be
      name: http
      protocol: TCP
      targetPort: 8080

### INGRESS ###
# host เดียวกับ FE แต่คนละ path — nginx merge เข้า server block เดียวให้เอง (D1)
ingress:
  enabled: true
  ingressClassName: 'nginx'
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "1G"   # multipart รูปเครื่องชาร์จ (≤1.2MB) — default ของ nginx = 1m
  # 🔴 ไม่ประกาศ tls: ที่นี่ — ให้ FE ประกาศฝั่งเดียว (D1 กติกาข้อ 3)
  hosts:
    - host: <hostname จริง>          # 🔴 ต้องเป็น string เดียวกันเป๊ะกับ values ของ FE
      paths:                         # 🔴 ครบ 5 กลุ่ม — ขาดข้อไหน path นั้นตกไป FE แล้ว 404 เงียบ
        - { path: /api,       pathType: Prefix, serviceName: evregist-be, servicePort: http }
        - { path: /thaid,     pathType: Prefix, serviceName: evregist-be, servicePort: http }
        - { path: /login,     pathType: Exact,  serviceName: evregist-be, servicePort: http }
        - { path: /logout,    pathType: Exact,  serviceName: evregist-be, servicePort: http }
        - { path: /dashboard, pathType: Exact,  serviceName: evregist-be, servicePort: http }

### VAULT ###
vaultStaticSecret:
  enabled: true
  name: evregist-be-secret
  mount: evregist
  path: dev/api/evregist-be-secret
```

---

## 6. ต้องขอ / ต้องถามใครบ้าง

### DevSecOps

- [ ] 🔴 **สร้าง repo backend บน GitLab** — ยังไม่มี · ต้องได้ `Dockerfile.base` + `.gitlab-ci.yml` ของ template Go
- [ ] 🔴 **ยืนยันว่า Ingress 2 ตัวบน host เดียวกันแบ่ง path กันได้** (D1) — ควรได้อยู่แล้วเพราะ
      คอมเมนต์ใน values ตัวอย่างเขียนเองว่า *"สามารถใช้แค่ ingress ในที่เดียว แล้วเพิ่ม proxy path
      เพื่อไป service ต่างๆ"* แต่ขอให้ยืนยันว่าเปิด **2 ตัว** แล้ว merge ได้ ไม่ติด policy อะไร
- [ ] 🔴 **TLS terminate ที่ไหน — ingress หรือ LB ข้างหน้า** · ตัวอย่างไม่มี `tls:` block เลย
      → ผูกกับ `COOKIE_SECURE=true` โดยตรง ถ้าเป็น HTTP ล้วน **login พังทั้งระบบ**
      · ถ้า terminate ที่ ingress → **ประกาศ `tls:` ที่ FE ฝั่งเดียว** (D1 กติกาข้อ 3)
- [ ] 🔴 **hostname จริงของแต่ละ env** — ตัวอย่างเป็น placeholder `evregist-dev.com`
- [ ] **สิทธิ์เขียน Vault** mount `evregist` path `{dev,uat,prod}/api/evregist-be-secret`
- [ ] **`image.repository` ใช้ `evregist/web/evregist-be` จริงไหม** — "web/" สำหรับ backend ดูเหมือน copy มาจาก FE
- [ ] **base image Go** ที่ registry ภายในมี tag อะไรบ้าง (ต้อง ≥ 1.25 — B1)
- [ ] **เปิด readinessProbe ไหม** — ตัวตัดสินว่าต้องทำ B3 หรือไม่
- [ ] **egress policy** — backend ต้องออกไป DOPA, Keycloak, geodrive:9021, Postgres

### DOPA (ThaID)

- [ ] 🔴 ลงทะเบียน `THAID_REDIRECT_URI` ใหม่เป็น `https://<hostname>/thaid/callback`
      (ปัจจุบัน sandbox ล็อกไว้ที่ `http://localhost:3000/*`)

### ทีม Keycloak

- [ ] 🔴 เพิ่ม redirect URI `https://<hostname>/dashboard` และ post-logout URI

### DBA

- [ ] Postgres เป็น managed service หรือ StatefulSet · host/port/user/db name ของแต่ละ env

---

## 7. ก่อน merge เข้า `main` — บังคับทำ

> pipeline **ไม่รันบน Merge Request** (`workflow.rules` = `main`/tag) และแก้ไม่ได้
> → **merge = build + deploy ลง dev จริง ไม่มีจังหวะให้ทดสอบระหว่างทาง**

```bash
go build ./...
go vet ./...

# จำลอง build ของ pipeline
docker build -t evregist-be:local .
docker images evregist-be:local          # ต้อง < 100MB ถ้าแก้ B2 แล้ว
docker run --rm evregist-be:local id     # ต้องไม่ใช่ uid 0 (non-root policy)

# จำลอง SQA gate
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  aquasec/trivy image --severity HIGH,CRITICAL evregist-be:local
```

**Definition of Done:**
- [ ] `docker build` ผ่าน (image < 100MB, non-root, มี ca-certificates)
- [ ] รัน compose คู่กับ frontend ที่ `NEXT_PUBLIC_API_URL=""` แล้ว ThaID login + campaign gate ยังทำงาน
- [ ] ถ้าทำ B3: `curl localhost:<port>/healthz` = 200 · `/readyz` = 200 เมื่อ DB ต่อได้ / 503 เมื่อต่อไม่ได้

---

## 8. Smoke test หลัง deploy ลง dev

| # | ทดสอบ | จุดที่พิสูจน์ |
| --- | --- | --- |
| 1 | pod ขึ้น `Running` ไม่ CrashLoop | env ครบทั้ง 33 ตัว (D3) |
| 2 | `https://<host>/api/v1/campaign` ตอบ JSON | Ingress `/api` → `evregist-be` |
| 2b | 🔴 **`curl -I` ทั้ง 5 path** — `/api/v1/campaign` · `/thaid/login` · `/login` · `/logout` · `/dashboard` **ต้องไม่มีตัวไหนได้ 404 หน้า Next.js** | **D1 — path ครบไหม** · ตัวไหนได้ HTML 404 = ตกไป catch-all ของ FE |
| 2c | `https://<host>/` ได้หน้าเว็บ (ไม่ใช่ JSON ของ Gin) | FE catch-all ยังทำงาน ไม่โดน BE กลืน |
| 3 | ThaID login เต็มรอบจนได้ `citizen_session` | `THAID_REDIRECT_URI` + egress DOPA |
| 4 | CA lookup ตอบผลจริง | DB + `PEA_CS_SERVICE_URL` |
| 5 | ส่งฟอร์มพร้อมรูป (multipart) สำเร็จ | egress geodrive:9021 + S3 credential + `proxy-body-size` |
| 6 | เปิดรูปผ่าน presigned URL ได้ | `storage.PresignGet` |
| 7 | พนักงาน login Keycloak เข้า `/backoffice` | Ingress `/login` + `/dashboard` |
| 8 | Backoffice อนุมัติ/ปฏิเสธได้ | RBAC + cookie rotation |
| 9 | Cookie มี `Secure` + `HttpOnly` จริง | `ENV=production` + `COOKIE_SECURE=true` + TLS |
| 10 | ตาราง DB ถูกสร้างครบหลัง pod แรกขึ้น | `AutoMigrate` (replicas=1) |

```bash
kubectl -n <namespace> get pods -l app=evregist-be
kubectl -n <namespace> logs -f deploy/evregist-be
kubectl -n <namespace> get secret evregist-be-secret -o jsonpath='{.data}' | jq 'keys'   # ดูว่า Vault sync มาครบไหม
```

---

## 9. Rollback

| สถานการณ์ | การถอย |
| --- | --- |
| Deploy พัง | Argo rollback ไป revision ก่อนหน้า |
| Image ใหม่มีปัญหา | แก้ image tag ใน `values.dev.yml` กลับไป tag เดิม (แก้ที่ deployment repo ตรง ๆ) |
| Pipeline แดงค้างบน `main` | revert commit บน `main` → pipeline รันใหม่อัตโนมัติ |
| env/secret ผิด | แก้ Vault → VSO sync ใหม่ → `kubectl rollout restart deploy/evregist-be` |

> ⚠️ **AutoMigrate ไม่มี rollback** — ถ้า schema เปลี่ยนแล้วถอย image กลับ ตารางที่ migrate ไปแล้วไม่ย้อนกลับให้
> ก่อน deploy ที่แตะ model ต้อง backup DB ก่อนเสมอ
