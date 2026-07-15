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
	"teison":     "Teison", // TEISON/Teison — พบตอน QA §6.2
}

func canonBrand(b string) string {
	b = strings.TrimSpace(b)
	if v, ok := brandAlias[strings.ToLower(b)]; ok {
		return v
	}
	return b
}
