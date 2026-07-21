// cmd/seeddashboarddemo seeds sample GeneralInfo rows for the back-office
// dashboard tab (unclaimed pool + a demo officer's claimed/completed queue)
// so the tab has something to show in dev without waiting on real submissions.
// Idempotent: skips when the demo CA range already exists. Run: go run ./cmd/seeddashboarddemo
package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/models"
)

// Demo officer — a fake Keycloak sub, distinct from any real staff account, so
// seeded "claimed by me" rows only ever show up for someone who happens to log
// in with this sub (i.e. nobody in practice). They exist to populate the
// unclaimed pool and to show what a claimed/completed row looks like in the UI.
const demoOfficerSub = "demo-officer-1"
const demoOfficerName = "เจ้าหน้าที่สาธิต (demo)"

// All demo CAs share this prefix so the seeder can detect it already ran.
const demoCaPrefix = "990000000"

func points(n int) *int { return &n }

func main() {
	_ = godotenv.Load()
	if err := database.Connect(); err != nil {
		log.Fatal(err)
	}
	if err := database.DB.AutoMigrate(&models.GeneralInfo{}, &models.Employee{}); err != nil {
		log.Fatal(err)
	}

	var existing int64
	if err := database.DB.Model(&models.GeneralInfo{}).
		Where("ca LIKE ?", demoCaPrefix+"%").
		Count(&existing).Error; err != nil {
		log.Fatal(err)
	}
	if existing > 0 {
		log.Printf("demo dashboard data already seeded (%d row(s)) — nothing to do", existing)
		return
	}

	if err := database.DB.Where(models.Employee{Sub: demoOfficerSub}).
		Assign(models.Employee{Email: "demo.officer@pea.co.th", GivenName: "เจ้าหน้าที่", FamilyName: "สาธิต"}).
		FirstOrCreate(&models.Employee{}).Error; err != nil {
		log.Fatal(err)
	}

	now := time.Now().UTC()
	claimedAt := now.Add(-2 * time.Hour)
	reviewedAt := now.Add(-24 * time.Hour)

	rows := []models.GeneralInfo{
		// รอรับ — unclaimed pool (visible to any staff, claimable by whoever clicks first)
		{PID: "1100000000001", FirstName: "สมชาย", LastName: "ใจดี", Address: "12 ถ.สุขุมวิท กรุงเทพฯ", Ca: demoCaPrefix + "001", EntrySource: "thaid", Status: "pending"},
		{PID: "1100000000002", FirstName: "สมหญิง", LastName: "รักไทย", Address: "45 ถ.พระราม 4 กรุงเทพฯ", Ca: demoCaPrefix + "002", EntrySource: "smartplus", Status: "pending"},
		{PID: "1100000000003", FirstName: "วิชัย", LastName: "ศรีสุข", Address: "78 ถ.เพชรบุรี กรุงเทพฯ", Ca: demoCaPrefix + "003", EntrySource: "thaid", Status: "pending"},
		{PID: "1100000000004", FirstName: "มานี", LastName: "มีนา", Address: "9 ถ.รัชดาภิเษก กรุงเทพฯ", Ca: demoCaPrefix + "004", EntrySource: "thaid", Status: "needs_info"},
		{PID: "1100000000005", FirstName: "ประยุทธ", LastName: "แสงทอง", Address: "23 ถ.ลาดพร้าว กรุงเทพฯ", Ca: demoCaPrefix + "005", EntrySource: "smartplus", Status: "pending"},

		// งานของฉัน (active) — claimed by the demo officer, still in progress
		{PID: "1100000000006", FirstName: "อรุณ", LastName: "เช้าวัน", Address: "56 ถ.งามวงศ์วาน นนทบุรี", Ca: demoCaPrefix + "006", EntrySource: "thaid", Status: "pending",
			ClaimedBy: demoOfficerSub, ClaimedName: demoOfficerName, ClaimedAt: &claimedAt},

		// งานของฉัน (completed) — decided by the demo officer, claim slot freed
		{PID: "1100000000007", FirstName: "กมล", LastName: "บุญมาก", Address: "34 ถ.บางนา กรุงเทพฯ", Ca: demoCaPrefix + "007", EntrySource: "smartplus", Status: "approved",
			ReviewedBy: demoOfficerSub, ReviewedAt: &reviewedAt, PointsAwarded: points(500)},
		{PID: "1100000000008", FirstName: "ดวงใจ", LastName: "งามพร้อม", Address: "67 ถ.รามคำแหง กรุงเทพฯ", Ca: demoCaPrefix + "008", EntrySource: "thaid", Status: "rejected",
			ReviewedBy: demoOfficerSub, ReviewedAt: &reviewedAt, PointsAwarded: points(0)},
	}

	if err := database.DB.Create(&rows).Error; err != nil {
		log.Fatal(err)
	}
	log.Printf("seeded %d demo dashboard row(s) (CA prefix %s, demo officer sub %q)", len(rows), demoCaPrefix, demoOfficerSub)
}
