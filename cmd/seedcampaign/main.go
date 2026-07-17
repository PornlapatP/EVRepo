// cmd/seedcampaign seeds the single active registration campaign window for
// dev/test. Idempotent: skips when an active campaign already exists (the model
// has no natural unique key to ON CONFLICT on, and re-running must never clobber
// a window staff already tuned in the back-office).  Run: go run ./cmd/seedcampaign
package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/models"
)

// A wide window so the form is "open" locally; the back-office can narrow it.
// Stored UTC per the project's date rule (models/campaign.go).
var seedCampaign = models.Campaign{
	Name:     "แคมเปญลงทะเบียน EV Wall Box (dev)",
	StartAt:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	EndAt:    time.Date(2027, 12, 31, 23, 59, 59, 0, time.UTC),
	IsActive: true,
}

func main() {
	_ = godotenv.Load()
	if err := database.Connect(); err != nil {
		log.Fatal(err)
	}
	if err := database.DB.AutoMigrate(&models.Campaign{}); err != nil {
		log.Fatal(err)
	}

	var active int64
	if err := database.DB.Model(&models.Campaign{}).
		Where("is_active = ?", true).
		Count(&active).Error; err != nil {
		log.Fatal(err)
	}
	if active > 0 {
		log.Printf("active campaign already exists (%d row(s)) — nothing to seed", active)
		return
	}

	row := seedCampaign
	if err := database.DB.Create(&row).Error; err != nil {
		log.Fatal(err)
	}
	log.Printf("seeded campaign %q [%s .. %s]", row.Name,
		row.StartAt.Format(time.RFC3339), row.EndAt.Format(time.RFC3339))
}
