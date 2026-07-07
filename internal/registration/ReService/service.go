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

// CreateGeneralInfoWithRelations is the only place a GeneralInfo row is ever
// persisted — CheckCA is read-only (see below) and just previews the data.
// If this ca+pid has no GeneralInfo row yet, one is created here (re-querying
// PEA's cs-service for the real name/address) before chargers/EVs are
// attached to it. pid comes from the authenticated ThaID session
// (CitizenAuthMiddleware) — never trusted from the request body.
func (s *GeneralService) CreateGeneralInfoWithRelations(
	ctx context.Context,
	req *model.CreateGeneralInfoRequest,
	pid string,
) error {

	return s.db.Transaction(func(tx *gorm.DB) error {

		var general models.GeneralInfo
		err := tx.Where("ca = ? AND pid = ?", req.Ca, pid).First(&general).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			detail, err := s.peaCS.GetCustomerDetail(ctx, req.Ca)
			if err != nil {
				return err
			}
			if !detail.Success || detail.Data == nil {
				return ErrCANotFound
			}

			general = models.GeneralInfo{
				PID:       pid,
				FirstName: detail.Data.FirstName,
				LastName:  detail.Data.LastName,
				Address:   detail.Data.Address.FullAddress,
				Ca:        req.Ca,
			}
			if err := tx.Create(&general).Error; err != nil {
				return err
			}
		} else if err != nil {
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

// CheckCA is read-only — it never writes to the database. It looks up a CA
// number: if it already has a registration on file, the existing record
// (including its chargers/EVs, so the wizard can show a read-only "you
// already registered this" summary) is returned — not an error — the citizen
// can keep adding more chargers/EVs to it, "resuming" rather than being
// blocked. Otherwise it queries PEA's real customer master data (cs-service);
// if found there, an unsaved GeneralInfo is built from that real name/address
// purely as a preview so the wizard can show it before the citizen fills in
// the rest of the form. The row itself is only ever persisted by
// CreateGeneralInfoWithRelations, once the citizen submits the completed form.
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

	return &general, nil
}
