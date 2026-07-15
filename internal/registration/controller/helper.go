package controller

import (
	"context"
	"log"

	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/registration/model"
	"github.com/pornlapatP/EV/internal/storage"
)

func ToGeneralInfoResponse(ctx context.Context, m models.GeneralInfo, storageSvc *storage.Service) model.GeneralInfoResponse {
	return model.GeneralInfoResponse{
		ID:        m.ID,
		FirstName: m.FirstName,
		LastName:  m.LastName,
		Address:   m.Address,
		Ca:        m.Ca,
		Status:    m.Status,
		Chargers:  toChargerResponses(ctx, m.Chargers, storageSvc),
		Evs:       toEvResponses(m.Evs),
	}
}

func toChargerResponses(ctx context.Context, chargers []models.Charger, storageSvc *storage.Service) []model.ChargerResponse {
	res := make([]model.ChargerResponse, 0, len(chargers))
	for _, c := range chargers {
		res = append(res, model.ChargerResponse{
			ID:            c.ID,
			SerialNumber:  c.SerialNumber,
			ConnectorType: c.ConnectorType,
			Kw:            c.Kw,
			ImageURL:      presignOrEmpty(ctx, storageSvc, c.ImageKey),
			LabelImageURL: presignOrEmpty(ctx, storageSvc, c.LabelImageKey),
			Brand:         c.Brand,
			Model:         c.Model,
			Vendor:        toVendorResponse(c.Vendor),
		})
	}
	return res
}

// presignOrEmpty signs a short-lived read URL for an S3 key. Presigning is a
// local signature computation (no network call), so failures here only mean
// missing config — log and degrade to an empty string rather than failing
// the whole response.
func presignOrEmpty(ctx context.Context, storageSvc *storage.Service, key string) string {
	if key == "" {
		return ""
	}
	url, err := storageSvc.PresignGet(ctx, key, storage.DefaultPresignTTL)
	if err != nil {
		log.Printf("presign %q failed: %v", key, err)
		return ""
	}
	return url
}

func toEvResponses(evs []models.Ev) []model.EvResponse {
	res := make([]model.EvResponse, 0, len(evs))
	for _, e := range evs {
		res = append(res, model.EvResponse{
			ID:                 e.ID,
			Brand:              e.Brand,
			Model:              e.Model,
			Year:               e.Year,
			Battery:            e.Battery,
			ChargingPeriod:     e.ChargingPeriod,
			ChargingStartTime:  e.ChargingStartTime,
			ChargingFinishTime: e.ChargingFinishTime,
			Vendor:             toVendorResponse(e.Vendor),
		})
	}
	return res
}

func toVendorResponse(v models.Vendor) model.VendorResponse {
	return model.VendorResponse{
		ID:         v.ID,
		VendorName: v.VendorName,
		Country:    v.Country,
	}
}
