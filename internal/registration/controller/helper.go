package controller

import (
	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/registration/model"
)

func ToGeneralInfoResponse(m models.GeneralInfo) model.GeneralInfoResponse {
	return model.GeneralInfoResponse{
		ID:        m.ID,
		FirstName: m.FirstName,
		LastName:  m.LastName,
		Address:   m.Address,
		Ca:        m.Ca,
		Chargers:  toChargerResponses(m.Chargers),
		Evs:       toEvResponses(m.Evs),
	}
}

func toChargerResponses(chargers []models.Charger) []model.ChargerResponse {
	res := make([]model.ChargerResponse, 0, len(chargers))
	for _, c := range chargers {
		res = append(res, model.ChargerResponse{
			ID:           c.ID,
			SerialNumber: c.SerialNumber,
			Vendor:       toVendorResponse(c.Vendor),
		})
	}
	return res
}

func toEvResponses(evs []models.Ev) []model.EvResponse {
	res := make([]model.EvResponse, 0, len(evs))
	for _, e := range evs {
		res = append(res, model.EvResponse{
			ID:          e.ID,
			PlateNumber: e.PlateNumber,
			Province:    e.Province,
			Brand:       e.Brand,
			Model:       e.Model,
			Vendor:      toVendorResponse(e.Vendor),
		})
	}
	return res
}

func toVendorResponse(v any) model.VendorResponse {
	switch vendor := v.(type) {
	case models.VendorCharge:
		return model.VendorResponse{
			ID:         vendor.ID,
			VendorName: vendor.VendorName,
			Country:    vendor.Country,
		}
	case models.VendorEv:
		return model.VendorResponse{
			ID:         vendor.ID,
			VendorName: vendor.VendorName,
			Country:    vendor.Country,
		}
	default:
		return model.VendorResponse{}
	}
}
