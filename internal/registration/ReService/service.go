package regisservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/peacs"
	"github.com/pornlapatP/EV/internal/registration/model"
	"gorm.io/gorm"
)

type GeneralService struct {
	db    *gorm.DB
	peaCS *peacs.Client
}

func NewGeneralService(db *gorm.DB, peaCS *peacs.Client) *GeneralService {
	return &GeneralService{db: db, peaCS: peaCS}
}

// CreateGeneralInfoWithRelations attaches chargers/EVs to the GeneralInfo row
// that CheckCA already created for this ca+pid — it does NOT create a new
// GeneralInfo itself. pid comes from the authenticated ThaID session
// (CitizenAuthMiddleware) — never trusted from the request body.
func (s *GeneralService) CreateGeneralInfoWithRelations(
	req *model.CreateGeneralInfoRequest,
	pid string,
) error {

	return s.db.Transaction(func(tx *gorm.DB) error {

		var general models.GeneralInfo
		if err := tx.Where("ca = ? AND pid = ?", req.Ca, pid).First(&general).Error; err != nil {
			return fmt.Errorf("กรุณาตรวจสอบเลข CA ก่อนส่งข้อมูลลงทะเบียน")
		}

		for _, c := range req.Chargers {

			vendorID, err := s.getOrCreateVendor(tx, c.VendorID, c.Vendor, vendorTypeCharger)
			if err != nil {
				return err
			}

			charger := models.Charger{
				GeneralInfoID: general.ID,
				VendorID:      vendorID,
				SerialNumber:  c.SerialNumber,
				ConnectorType: c.ConnectorType,
				Kw:            c.Kw,
				ImageKey:      c.ImageKey,
				LabelImageKey: c.LabelImageKey,
				Brand:         c.Brand,
				Model:         c.Model,
			}

			if err := tx.Create(&charger).Error; err != nil {
				return err
			}
		}

		for _, e := range req.Evs {

			vendorID, err := s.getOrCreateVendor(tx, e.VendorID, e.Vendor, vendorTypeEv)
			if err != nil {
				return err
			}

			ev := models.Ev{
				GeneralInfoID:      general.ID,
				VendorID:           vendorID,
				PlateNumber:        e.PlateNumber,
				Province:           e.Province,
				Brand:              e.Brand,
				Model:              e.Model,
				Year:               e.Year,
				Battery:            e.Battery,
				ChargingPeriod:     e.Charging.Period,
				ChargingStartTime:  e.Charging.StartTime,
				ChargingFinishTime: e.Charging.FinishTime,
			}

			if err := tx.Create(&ev).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *GeneralService) GetAllGeneralInfo() ([]models.GeneralInfo, error) {
	var result []models.GeneralInfo

	err := s.db.
		Preload("Chargers.Vendor").
		Preload("Evs.Vendor").
		Find(&result).Error

	return result, err
}

// GetGeneralInfoByPID returns every registration (one per CA) submitted by a
// given citizen.
func (s *GeneralService) GetGeneralInfoByPID(pid string) ([]models.GeneralInfo, error) {
	var result []models.GeneralInfo

	err := s.db.
		Where("pid = ?", pid).
		Preload("Chargers.Vendor").
		Preload("Evs.Vendor").
		Find(&result).Error

	return result, err
}

var ErrCANotFound = errors.New("ca not found")

// CheckCA looks up a CA number: if it already has a registration on file,
// the existing record (including its chargers/EVs, so the wizard can show a
// read-only "you already registered this" summary) is returned — not an
// error — the citizen can keep adding more chargers/EVs to it, "resuming"
// rather than being blocked. Otherwise it queries PEA's real customer master
// data (cs-service); if found there, a new GeneralInfo row is created
// immediately using that real name/address so the rest of the wizard has
// somewhere to attach chargers/EVs.
func (s *GeneralService) CheckCA(ctx context.Context, ca, pid string) (*models.GeneralInfo, error) {
	var existing models.GeneralInfo
	err := s.db.
		Preload("Chargers.Vendor").
		Preload("Evs.Vendor").
		Where("ca = ?", ca).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	detail, err := s.peaCS.GetCustomerDetail(ctx, ca)
	if err != nil {
		return nil, err
	}
	if !detail.Success || detail.Data == nil {
		return nil, ErrCANotFound
	}

	general := models.GeneralInfo{
		PID:       pid,
		FirstName: detail.Data.FirstName,
		LastName:  detail.Data.LastName,
		Address:   detail.Data.Address.FullAddress,
		Ca:        ca,
	}
	if err := s.db.Create(&general).Error; err != nil {
		return nil, err
	}

	return &general, nil
}
