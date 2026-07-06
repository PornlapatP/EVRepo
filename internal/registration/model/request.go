package model

// CreateGeneralInfoRequest is the "data" part of the multipart registration
// submission. Identity fields (name/address) are NOT part of this payload —
// the frontend's registration wizard never collects them; they come from the
// citizen's ThaID session instead (see CitizenAuthMiddleware).
type CreateGeneralInfoRequest struct {
	Ca string `json:"ca" binding:"required"`

	Chargers []CreateChargerRequest `json:"chargers"`
	Evs      []CreateEvRequest      `json:"evs"`
}

type CreateChargerRequest struct {
	VendorID      *uint                `json:"vendorId,omitempty"`
	Vendor        *CreateVendorRequest `json:"vendor,omitempty"`
	SerialNumber  string               `json:"serialNumber" binding:"required"`
	ConnectorType string               `json:"connectorType" binding:"required"`
	Kw            string               `json:"kw" binding:"required"`
	Brand         string               `json:"brand"`
	Model         string               `json:"model"`

	// S3 object keys, populated server-side after the matching multipart file
	// parts are uploaded — never bound from the "data" JSON part itself.
	ImageKey      string `json:"-"`
	LabelImageKey string `json:"-"`
}

type CreateEvRequest struct {
	VendorID *uint                `json:"vendorId,omitempty"`
	Vendor   *CreateVendorRequest `json:"vendor,omitempty"`
	// PlateNumber/Province are optional — the frontend wizard doesn't collect them today.
	PlateNumber string `json:"plateNumber"`
	Province    string `json:"province"`
	Brand       string `json:"brand"`
	Model       string `json:"model"`
	Year        string `json:"year" binding:"required"`
	Battery     string `json:"battery" binding:"required"`

	Charging CreateChargingRequest `json:"charging" binding:"required"`
}

type CreateChargingRequest struct {
	Period     string `json:"period" binding:"required"`
	StartTime  string `json:"startTime" binding:"required"`
	FinishTime string `json:"finishTime" binding:"required"`
}

type CreateVendorRequest struct {
	VendorName string `json:"vendorName" binding:"required"`
	Country    string `json:"country"`
}
