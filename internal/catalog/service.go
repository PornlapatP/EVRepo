// Package catalog serves read-only master catalogs to the frontend for the
// registration wizard's cascading dropdowns. It is public reference data (no
// auth): the EV catalog (master_evs) as a brand → models → batteries tree, and
// the charger catalog (master_chargers) as a brand → models tree that carries
// each model's spec for auto-fill.
package catalog

import (
	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// BatteryDTO / ModelDTO / BrandDTO are the camelCase JSON contract the frontend
// consumes directly. `label` is a bare number (no unit) — the FE renders
// `${label} kWh`. It's the only battery field: the value shown in the dropdown,
// stored in evs.battery, and used to resolve evs.master_ev_id.
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

// GetCatalog reads master_evs once (ordered brand, model, id) and nests it in a
// single pass — the ordering guarantees rows for the same brand/model are
// contiguous, so no N+1 / Preload is needed.
func (s *Service) GetCatalog() ([]BrandDTO, error) {
	var rows []models.MasterEV
	if err := s.db.Order("brand, model, id").Find(&rows).Error; err != nil {
		return nil, err
	}

	brands := make([]BrandDTO, 0)
	for _, r := range rows {
		bat := BatteryDTO{Label: r.BatteryLabel}

		// same brand as the last one? (rows are ordered, so only the tail can match)
		if n := len(brands); n > 0 && brands[n-1].Brand == r.Brand {
			b := &brands[n-1]
			if m := len(b.Models); m > 0 && b.Models[m-1].Model == r.Model {
				b.Models[m-1].Batteries = append(b.Models[m-1].Batteries, bat)
			} else {
				b.Models = append(b.Models, ModelDTO{Model: r.Model, Batteries: []BatteryDTO{bat}})
			}
			continue
		}

		brands = append(brands, BrandDTO{
			Brand:  r.Brand,
			Models: []ModelDTO{{Model: r.Model, Batteries: []BatteryDTO{bat}}},
		})
	}

	return brands, nil
}

// ChargerModelDTO is one certified charger model plus the spec the form
// auto-fills when it's picked. `currentType` is the AC/DC value the form's
// (confusingly named) `connectorType` field wants; `ratedPowerKw` feeds
// `kw`. `connectorType` here is the physical head (CCS2/CHAdeMO/AC_TYPE_2),
// kept for a future form field — see docs/master-charger-seed-plan.md §2.
type ChargerModelDTO struct {
	Model           string `json:"model"`
	CurrentType     string `json:"currentType"`
	ConnectorType   string `json:"connectorType"`
	TotalConnectors int    `json:"totalConnectors"`
	RatedPowerKw    string `json:"ratedPowerKw"`
}

type ChargerBrandDTO struct {
	Brand  string            `json:"brand"`
	Models []ChargerModelDTO `json:"models"`
}

// GetChargerCatalog reads master_chargers once (ordered brand, model) and nests
// it into brand → models in a single pass. MasterCharger grain is already
// model-level (one row per brand/model), so no deeper nesting is needed.
func (s *Service) GetChargerCatalog() ([]ChargerBrandDTO, error) {
	var rows []models.MasterCharger
	if err := s.db.Order("brand, model").Find(&rows).Error; err != nil {
		return nil, err
	}

	brands := make([]ChargerBrandDTO, 0)
	for _, r := range rows {
		m := ChargerModelDTO{
			Model:           r.Model,
			CurrentType:     r.CurrentType,
			ConnectorType:   r.ConnectorType,
			TotalConnectors: r.TotalConnectors,
			RatedPowerKw:    r.RatedPowerKw,
		}

		// same brand as the last one? (rows are ordered, so only the tail can match)
		if n := len(brands); n > 0 && brands[n-1].Brand == r.Brand {
			brands[n-1].Models = append(brands[n-1].Models, m)
			continue
		}

		brands = append(brands, ChargerBrandDTO{Brand: r.Brand, Models: []ChargerModelDTO{m}})
	}

	return brands, nil
}
