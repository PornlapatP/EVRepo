// Package peacs calls PEA's internal customer-service API (cs-service.pea.co.th)
// to look up the real account holder behind a CA (electricity account) number —
// the "mother DB" that this system otherwise has no integration with.
package peacs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const defaultBaseURL = "https://cs-service.pea.co.th/api/customer/detail"

type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a Client. Override the endpoint via PEA_CS_SERVICE_URL if needed
// (e.g. pointing at a mock server for local dev/testing).
func New() *Client {
	baseURL := os.Getenv("PEA_CS_SERVICE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type customerDetailRequest struct {
	BpNo               *string `json:"bpNo"`
	CaNo               string  `json:"caNo"`
	IsActiveContract   bool    `json:"isActiveContract"`
	IsSearchFromAllPea bool    `json:"isSearchFromAllPea"`
}

type CustomerAddress struct {
	FullAddress string `json:"fullAddress"`
}

// CustomerDetail is only the subset of cs-service's response this app needs
// (the real payload has many more meter/contract fields we don't store).
type CustomerDetail struct {
	CaNo      string          `json:"caNo"`
	Name      string          `json:"name"`
	FirstName string          `json:"firstName"`
	LastName  string          `json:"lastName"`
	Address   CustomerAddress `json:"address"`

	// PEA branch + business-type fields — captured for grid-planning
	// reporting (design/07-reports-analytics.md), not shown in any UI yet.
	PeaName          string `json:"peaName"`          // เช่น "กฟจ.ตรัง"
	CaName           string `json:"caName"`           // ชื่อเจ้าของ CA แบบเต็ม (ต่างจาก FirstName/LastName ที่แยกคำ)
	PeaOffice        string `json:"peaOffice"`        // รหัสเขต เช่น "KTRU"
	BpNo             string `json:"bpNo"`             // Business Partner No
	BusinessType     string `json:"businessType"`     // เช่น "TSIC"
	BusinessTypeCode string `json:"businessTypeCode"` // เช่น "00001"
	BusinessTypeText string `json:"businessTypeText"` // เช่น "บ้านอยู่อาศัย"
}

type CustomerDetailResponse struct {
	Data    *CustomerDetail `json:"data"`
	Success bool            `json:"success"`
	Message string          `json:"message"`
}

// GetCustomerDetail looks up a CA number. A "not found" CA is NOT a Go error —
// it comes back as CustomerDetailResponse{Success:false, Data:nil}; only
// network/transport/decode failures return an error.
func (c *Client) GetCustomerDetail(ctx context.Context, caNo string) (*CustomerDetailResponse, error) {
	body, err := json.Marshal(customerDetailRequest{
		CaNo:               caNo,
		IsActiveContract:   false,
		IsSearchFromAllPea: true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cs-service request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build cs-service request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cs-service request failed: %w", err)
	}
	defer resp.Body.Close()

	var result CustomerDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode cs-service response: %w", err)
	}

	return &result, nil
}
