package regisservice

import (

	// "log"

	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/registration/model"
	"gorm.io/gorm"
)

type GeneralService struct {
	db *gorm.DB
}

func NewGeneralService(db *gorm.DB) *GeneralService {
	return &GeneralService{db: db}
}

func (s *GeneralService) CreateGeneralInfoWithRelations(
	req *model.CreateGeneralInfoRequest,
) error {

	return s.db.Transaction(func(tx *gorm.DB) error {

		general := models.GeneralInfo{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Address:   req.Address,
			Ca:        req.Ca,
		}

		if err := tx.Create(&general).Error; err != nil {
			return err
		}

		for _, c := range req.Chargers {

			vendorID, err := s.getOrCreateVendorCharge(tx, c)
			if err != nil {
				return err
			}

			charger := models.Charger{
				UserID:       general.ID,
				VendorID:     vendorID,
				SerialNumber: c.SerialNumber,
			}

			if err := tx.Create(&charger).Error; err != nil {
				return err
			}
		}

		for _, e := range req.Evs {

			vendorID, err := s.getOrCreateVendorEv(tx, e)
			if err != nil {
				return err
			}

			ev := models.Ev{
				UserID:      general.ID,
				VendorID:    vendorID,
				PlateNumber: e.PlateNumber,
				Province:    e.Province,
				Brand:       e.Brand,
				Model:       e.Model,
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

// func (s *GeneralService) GetGeneralInfoByID(id uint) (*models.GeneralInfo, error) {
// 	var result models.GeneralInfo

// 	err := s.db.
// 		Preload("Chargers.Vendor").
// 		Preload("Evs.Vendor").
// 		First(&result, id).Error

// 	if err != nil {
// 		return nil, err
// 	}

// 	return &result, nil
// }
