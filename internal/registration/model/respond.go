package model

type GeneralInfoResponse struct {
	ID        uint   `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Address   string `json:"address"`
	Ca        string `json:"ca"`

	Chargers []ChargerResponse `json:"chargers"`
	Evs      []EvResponse      `json:"evs"`
}

type ChargerResponse struct {
	ID           uint           `json:"id"`
	SerialNumber string         `json:"serialNumber"`
	Vendor       VendorResponse `json:"vendor"`
}

type EvResponse struct {
	ID          uint           `json:"id"`
	PlateNumber string         `json:"plateNumber"`
	Province    string         `json:"province"`
	Brand       string         `json:"brand"`
	Model       string         `json:"model"`
	Vendor      VendorResponse `json:"vendor"`
}

type VendorResponse struct {
	ID         uint   `json:"id"`
	VendorName string `json:"vendorName"`
	Country    string `json:"country"`
}
