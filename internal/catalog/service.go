// Package catalog serves the read-only EV master catalog (master_evs) to the
// frontend as a nested brand → models → batteries tree, for the registration
// wizard's cascading dropdowns. It is public reference data (no auth).
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
