# scripts/seed-dev.ps1 — เวอร์ชัน Windows ของ seed-dev.sh
#   .\scripts\seed-dev.ps1          # เฉพาะที่จำเป็น
#   .\scripts\seed-dev.ps1 -Demo    # + ข้อมูลตัวอย่าง dashboard
param([switch]$Demo)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

Write-Host "==> [1/4] RBAC (roles / policies / master users)"
go run ./cmd/seedrbac
if ($LASTEXITCODE -ne 0) { throw "seedrbac failed" }

Write-Host "==> [2/4] Campaign window"
go run ./cmd/seedcampaign
if ($LASTEXITCODE -ne 0) { throw "seedcampaign failed" }

Write-Host "==> [3/4] Master charger catalog"
go run ./cmd/seedmaster
if ($LASTEXITCODE -ne 0) { throw "seedmaster failed" }

Write-Host "==> [4/4] Master EV catalog"
go run ./cmd/seedevmaster
if ($LASTEXITCODE -ne 0) { throw "seedevmaster failed" }

if ($Demo) {
  Write-Host "==> [demo] ตัวอย่างคำขอสำหรับหน้า dashboard"
  go run ./cmd/seeddashboarddemo
  if ($LASTEXITCODE -ne 0) { throw "seeddashboarddemo failed" }
}

Write-Host "==> done"
