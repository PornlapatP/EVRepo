package regisservice

import (
	"fmt"

	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/registration/model"
	"gorm.io/gorm"
)

func (s *GeneralService) getOrCreateVendorCharge(
	tx *gorm.DB,
	req model.CreateChargerRequest,
) (uint, error) {

	// case: use existing vendor
	if req.VendorID != nil {
		var vendor models.VendorCharge
		if err := tx.First(&vendor, *req.VendorID).Error; err != nil {
			return 0, fmt.Errorf("vendor charge not found: %d", *req.VendorID)
		}
		return vendor.ID, nil
	}

	// case: create new vendor
	if req.Vendor != nil {
		vendor := models.VendorCharge{
			VendorName: req.Vendor.VendorName,
			Country:    req.Vendor.Country,
		}

		if err := tx.Create(&vendor).Error; err != nil {
			return 0, err
		}

		return vendor.ID, nil
	}

	return 0, fmt.Errorf("vendorId or vendor is required for charger")
}

func (s *GeneralService) getOrCreateVendorEv(
	tx *gorm.DB,
	req model.CreateEvRequest,
) (uint, error) {

	// case: use existing vendor
	if req.VendorID != nil {
		var vendor models.VendorEv
		if err := tx.First(&vendor, *req.VendorID).Error; err != nil {
			return 0, fmt.Errorf("vendor ev not found: %d", *req.VendorID)
		}
		return vendor.ID, nil
	}

	// case: create new vendor
	if req.Vendor != nil {
		vendor := models.VendorEv{
			VendorName: req.Vendor.VendorName,
			Country:    req.Vendor.Country,
		}

		if err := tx.Create(&vendor).Error; err != nil {
			return 0, err
		}

		return vendor.ID, nil
	}

	return 0, fmt.Errorf("vendorId or vendor is required for ev")
}
