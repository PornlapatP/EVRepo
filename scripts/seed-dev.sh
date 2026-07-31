#!/usr/bin/env bash
# scripts/seed-dev.sh — seed ทุกอย่างที่ dev stage ต้องมีเพื่อให้ระบบใช้งานได้จริง
#
# ต้องรันจาก repo root (มี go.mod + .env ที่ชี้ DB ของ dev)
#   ./scripts/seed-dev.sh          # เฉพาะที่จำเป็น (required)
#   ./scripts/seed-dev.sh --demo   # + ข้อมูลตัวอย่างสำหรับ dashboard/back-office
#
# ทุก seeder idempotent — รันซ้ำได้ปลอดภัย
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> [1/4] RBAC (roles / policies / master users) — ไม่มีตัวนี้ back-office ติด 403 NO_ROLE"
go run ./cmd/seedrbac

echo "==> [2/4] Campaign window — ไม่มี campaign active = ระบบ 'ปิด' ประชาชน submit ไม่ได้"
go run ./cmd/seedcampaign

echo "==> [3/4] Master charger catalog (chargers.json)"
go run ./cmd/seedmaster

echo "==> [4/4] Master EV catalog (ev_master_seed.json)"
go run ./cmd/seedevmaster

if [[ "${1:-}" == "--demo" ]]; then
  echo "==> [demo] ตัวอย่างคำขอสำหรับหน้า dashboard (CA ขึ้นต้น 990000000)"
  go run ./cmd/seeddashboarddemo
fi

echo "==> done"
