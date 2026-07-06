package regisservice

import (
	"fmt"

	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/registration/model"
	"gorm.io/gorm"
)

const (
	vendorTypeCharger = "charger"
	vendorTypeEv      = "ev"
)

// getOrCreateVendor resolves a vendor either by ID or by an inline
// {vendorName, country} payload, deduping inline vendors by (name, type)
// instead of always inserting a new row.
func (s *GeneralService) getOrCreateVendor(
	tx *gorm.DB,
	vendorID *uint,
	inline *model.CreateVendorRequest,
	vendorType string,
) (uint, error) {

	if vendorID != nil {
		var vendor models.Vendor
		if err := tx.First(&vendor, *vendorID).Error; err != nil {
			return 0, fmt.Errorf("vendor not found: %d", *vendorID)
		}
		return vendor.ID, nil
	}

	if inline != nil {
		var vendor models.Vendor
		err := tx.Where(models.Vendor{VendorName: inline.VendorName, Type: vendorType}).
			Attrs(models.Vendor{Country: inline.Country}).
			FirstOrCreate(&vendor).Error
		if err != nil {
			return 0, err
		}
		return vendor.ID, nil
	}

	return 0, fmt.Errorf("vendorId or vendor is required")
}
