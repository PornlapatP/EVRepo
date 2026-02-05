package model

type CreateGeneralInfoRequest struct {
	FirstName string `json:"firstName" binding:"required"`
	LastName  string `json:"lastName" binding:"required"`
	Address   string `json:"address" binding:"required"`
	Ca        string `json:"ca" binding:"required"`

	Chargers []CreateChargerRequest `json:"chargers"`
	Evs      []CreateEvRequest      `json:"evs"`
}

type CreateChargerRequest struct {
	VendorID     *uint                `json:"vendorId,omitempty"`
	Vendor       *CreateVendorRequest `json:"vendor,omitempty"`
	SerialNumber string               `json:"serialNumber" binding:"required"`
}

type CreateEvRequest struct {
	VendorID    *uint                `json:"vendorId,omitempty"`
	Vendor      *CreateVendorRequest `json:"vendor,omitempty"`
	PlateNumber string               `json:"plateNumber" binding:"required"`
	Province    string               `json:"province"`
	Brand       string               `json:"brand"`
	Model       string               `json:"model"`
}

type CreateVendorRequest struct {
	VendorName string `json:"vendorName" binding:"required"`
	Country    string `json:"country"`
}
