package model

type GeneralInfoResponse struct {
	ID        uint   `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Address   string `json:"address"`
	Ca        string `json:"ca"`
	Status    string `json:"status"`

	Chargers []ChargerResponse `json:"chargers"`
	Evs      []EvResponse      `json:"evs"`
}

type ChargerResponse struct {
	ID            uint           `json:"id"`
	SerialNumber  string         `json:"serialNumber"`
	ConnectorType string         `json:"connectorType"`
	Kw            string         `json:"kw"`
	ImageURL      string         `json:"imageUrl"`
	LabelImageURL string         `json:"labelImageUrl"`
	Brand         string         `json:"brand"`
	Model         string         `json:"model"`
	Vendor        VendorResponse `json:"vendor"`
}

type EvResponse struct {
	ID      uint   `json:"id"`
	Brand   string `json:"brand"`
	Model   string `json:"model"`
	Year    string `json:"year"`
	Battery string `json:"battery"`

	ChargingPeriod     string `json:"chargingPeriod"`
	ChargingStartTime  string `json:"chargingStartTime"`
	ChargingFinishTime string `json:"chargingFinishTime"`

	Vendor VendorResponse `json:"vendor"`
}

type VendorResponse struct {
	ID         uint   `json:"id"`
	VendorName string `json:"vendorName"`
	Country    string `json:"country"`
}
