package regisservice

import (
	"context"
	"errors"
	"fmt"

	authservice "github.com/pornlapatP/EV/internal/auth/service"
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
// Ca is globally unique, so this upserts on it: a CA with no row yet is
// created (re-querying PEA's cs-service for the real name/address); a CA
// that already has a row is an edit, which only EntrySourceSmartPlus may do
// (design/03-role-matrix.md §PLANNED — direct ThaID logins are view-only
// after their first submission) and replaces its chargers/EVs outright
// rather than appending to them. pid comes from the authenticated ThaID
// session (CitizenAuthMiddleware) — never trusted from the request body.
func (s *GeneralService) CreateGeneralInfoWithRelations(
	ctx context.Context,
	req *model.CreateGeneralInfoRequest,
	pid string,
	source authservice.EntrySource,
) error {

	return s.db.Transaction(func(tx *gorm.DB) error {

		// keptChargerIDs/keptEvIDs stay nil (all lookups false) on the
		// create-new-CA path — there's nothing existing to match against, so
		// every charger/EV below naturally takes the "insert new" branch.
		var keptChargerIDs, keptEvIDs map[uint]bool

		var general models.GeneralInfo
		err := tx.Where("ca = ?", req.Ca).First(&general).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			detail, err := s.peaCS.GetCustomerDetail(ctx, req.Ca)
			if err != nil {
				return err
			}
			if !detail.Success || detail.Data == nil {
				return ErrCANotFound
			}

			general = models.GeneralInfo{
				PID:              pid,
				FirstName:        detail.Data.FirstName,
				LastName:         detail.Data.LastName,
				Address:          detail.Data.Address.FullAddress,
				Ca:               req.Ca,
				EntrySource:      string(source),
				PeaName:          detail.Data.PeaName,
				CaName:           detail.Data.CaName,
				PeaOffice:        detail.Data.PeaOffice,
				BpNo:             detail.Data.BpNo,
				BusinessType:     detail.Data.BusinessType,
				BusinessTypeCode: detail.Data.BusinessTypeCode,
				BusinessTypeText: detail.Data.BusinessTypeText,
			}
			if err := tx.Create(&general).Error; err != nil {
				return err
			}
		case err != nil:
			return fmt.Errorf("กรุณาตรวจสอบเลข CA ก่อนส่งข้อมูลลงทะเบียน")
		default:
			// Editing an existing CA. Reconcile by ID instead of wiping
			// everything: a charger/EV the payload still references by ID is
			// kept (and updated in place, preserving its image if no new file
			// was uploaded — see controller.uploadChargerFile); anything not
			// referenced anymore was removed by the citizen and gets deleted;
			// anything with no ID is a brand-new addition.
			//
			// Own-only (decided 2026-07-14, design/03-role-matrix.md §Entry
			// Source): the *only* requirement to edit is being the CA's
			// original owner (PID match) — every login channel goes through
			// real ThaID OAuth now, so PID is equally trustworthy regardless
			// of entrySource; there's no separate channel gate anymore.
			if general.PID != pid {
				return ErrNotOwner
			}

			// WattdId is intentionally NOT touched here — the citizen wizard no
			// longer collects it (see registration.model.CreateGeneralInfoRequest),
			// so this request never carries a value for it. It's now managed
			// exclusively through the back-office (internal/admin PatchField);
			// blindly writing req.WattdId here would silently wipe out
			// whatever staff had set.
			if err := tx.Model(&models.GeneralInfo{}).
				Where("id = ?", general.ID).
				Update("entry_source", string(source)).Error; err != nil {
				return err
			}

			var existingChargers []models.Charger
			if err := tx.Where("general_info_id = ?", general.ID).Find(&existingChargers).Error; err != nil {
				return err
			}
			keptChargerIDs = make(map[uint]bool, len(existingChargers))
			existingChargerByID := make(map[uint]models.Charger, len(existingChargers))
			for _, ec := range existingChargers {
				existingChargerByID[ec.ID] = ec
			}
			for i, c := range req.Chargers {
				if c.ID == nil {
					continue
				}
				existingCharger, ok := existingChargerByID[*c.ID]
				if !ok {
					continue // foreign/stale ID — treated as a new charger below
				}
				keptChargerIDs[*c.ID] = true
				if req.Chargers[i].ImageKey == "" {
					req.Chargers[i].ImageKey = existingCharger.ImageKey
				}
				if req.Chargers[i].LabelImageKey == "" {
					req.Chargers[i].LabelImageKey = existingCharger.LabelImageKey
				}
			}
			for id := range existingChargerByID {
				if !keptChargerIDs[id] {
					if err := tx.Delete(&models.Charger{}, id).Error; err != nil {
						return err
					}
				}
			}

			var existingEvs []models.Ev
			if err := tx.Where("general_info_id = ?", general.ID).Find(&existingEvs).Error; err != nil {
				return err
			}
			keptEvIDs = make(map[uint]bool, len(existingEvs))
			existingEvIDs := make(map[uint]bool, len(existingEvs))
			for _, ee := range existingEvs {
				existingEvIDs[ee.ID] = true
			}
			for _, e := range req.Evs {
				if e.ID != nil && existingEvIDs[*e.ID] {
					keptEvIDs[*e.ID] = true
				}
			}
			for id := range existingEvIDs {
				if !keptEvIDs[id] {
					if err := tx.Delete(&models.Ev{}, id).Error; err != nil {
						return err
					}
				}
			}
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

			if c.ID != nil && keptChargerIDs[*c.ID] {
				charger.ID = *c.ID
				if err := tx.Model(&models.Charger{}).Where("id = ?", charger.ID).Updates(charger).Error; err != nil {
					return err
				}
				continue
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

			if e.ID != nil && keptEvIDs[*e.ID] {
				ev.ID = *e.ID
				if err := tx.Model(&models.Ev{}).Where("id = ?", ev.ID).Updates(ev).Error; err != nil {
					return err
				}
				continue
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

// ErrNotOwner is returned when a session tries to edit a CA that was
// registered by a *different* citizen (PID) — own-only, decided 2026-07-14
// (design/03-role-matrix.md §Entry Source): PID match is the sole edit
// requirement now that every login channel goes through real ThaID OAuth.
var ErrNotOwner = errors.New("editing this registration requires being its original owner")

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
