package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pornlapatP/EV/internal/auth/config"
)

// ThaIDService handles authentication with ThaID (DOPA Digital ID).
type ThaIDService struct {
	cfg *config.Config
}

// NewThaIDService creates a new ThaIDService instance.
func NewThaIDService(cfg *config.Config) *ThaIDService {
	return &ThaIDService{cfg: cfg}
}

// ThaIDTokenResponse represents the token response from ThaID. Per the DOPA
// sandbox manual §6.2.2, there is no separate userinfo endpoint — when the
// authorization request's scope omits "openid", every requested claim comes
// back as a flat field on this same token response (e.g. {"pid": "...",
// "name": "..."}), not from a follow-up call.
type ThaIDTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expire_in"` // DOPA's field name — not the usual OAuth "expires_in"
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`

	PID            string       `json:"pid"`              // เลขบัตรประชาชน 13 หลัก
	GivenName      string       `json:"given_name"`       // ชื่อ (ไทย)
	MiddleName     string       `json:"middle_name"`      // ชื่อกลาง (ไทย)
	FamilyName     string       `json:"family_name"`      // นามสกุล (ไทย)
	Name           string       `json:"name"`             // ชื่อเต็ม (ไทย)
	GivenNameEN    string       `json:"given_name_en"`    // ชื่อ (อังกฤษ)
	MiddleNameEN   string       `json:"middle_name_en"`   // ชื่อกลาง (อังกฤษ)
	FamilyNameEN   string       `json:"family_name_en"`   // นามสกุล (อังกฤษ)
	NameEN         string       `json:"name_en"`          // ชื่อเต็ม (อังกฤษ)
	Title          string       `json:"title"`            // คำนำหน้า (ไทย)
	TitleEN        string       `json:"title_en"`         // คำนำหน้า (อังกฤษ)
	Birthdate      string       `json:"birthdate"`        // วันเกิด
	Gender         string       `json:"gender"`           // เพศ
	Address        addressClaim `json:"address"`          // ที่อยู่ — DOPA sends either a plain string or {formatted, raw}
	IAL            string       `json:"ial"`              // Identity Assurance Level
	SmartcardCode  string       `json:"smartcard_code"`   // รหัส smartcard
	DateOfExpiry   string       `json:"date_of_expiry"`   // วันหมดอายุบัตร
	DateOfIssuance string       `json:"date_of_issuance"` // วันออกบัตร
}

// addressClaim tolerates both shapes DOPA is observed to send for the
// "address" claim: a plain string, or an object like house_address
// ({"formatted": "...", "raw": "..."} per manual §6.1.1 item 19). Falls back
// to the raw JSON text for any other shape rather than failing the whole
// token decode.
type addressClaim string

func (a *addressClaim) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = addressClaim(s)
		return nil
	}

	var obj struct {
		Formatted string `json:"formatted"`
		Raw       string `json:"raw"`
	}
	if err := json.Unmarshal(data, &obj); err == nil && (obj.Formatted != "" || obj.Raw != "") {
		if obj.Formatted != "" {
			*a = addressClaim(obj.Formatted)
		} else {
			*a = addressClaim(obj.Raw)
		}
		return nil
	}

	*a = addressClaim(data)
	return nil
}

// generateState creates a random state string for CSRF protection.
func generateState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// BuildLoginURL constructs the ThaID authorization URL.
// Returns the URL and the state parameter (to be stored in session/cookie for verification).
func (s *ThaIDService) BuildLoginURL() (string, string) {
	state := generateState()

	// ไม่ใส่ "openid" — ตาม DOPA sandbox manual §6.2.2 การไม่ขอ openid ทำให้ claim ที่ขอ
	// ทุกตัวถูกส่งกลับมาเป็น flat field บน token response โดยตรง (ไม่ต้อง decode id_token
	// หรือยิง userinfo เพิ่ม) ส่วน scope ที่เหลือทั้งหมดนี้เป็นชื่อที่ DOPA รองรับจริง (manual §6.1.1)
	scopes := strings.Join([]string{
		"pid",
		"name",
		"given_name",
		"middle_name",
		"family_name",
		"name_en",
		"given_name_en",
		"middle_name_en",
		"family_name_en",
		"title",
		"title_en",
		"birthdate",
		"gender",
		"address",
		"ial",
		"smartcard_code",
		"date_of_expiry",
		"date_of_issuance",
	}, " ")

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", s.cfg.ThaIDClientID)
	params.Set("redirect_uri", s.cfg.ThaIDRedirectURI)
	params.Set("scope", scopes)
	params.Set("state", state)

	return fmt.Sprintf("%s?%s", s.cfg.ThaIDAuthURL, params.Encode()), state
}

// ExchangeCode exchanges the authorization code for tokens.
func (s *ThaIDService) ExchangeCode(code string) (*ThaIDTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", s.cfg.ThaIDRedirectURI)
	data.Set("client_id", s.cfg.ThaIDClientID)
	data.Set("client_secret", s.cfg.ThaIDClientSecret)

	resp, err := http.PostForm(s.cfg.ThaIDTokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("thaid token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("thaid token exchange failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var token ThaIDTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("thaid token decode failed: %w", err)
	}

	return &token, nil
}

// Revoke revokes a ThaID token.
func (s *ThaIDService) Revoke(token string) error {
	data := url.Values{}
	data.Set("token", token)
	data.Set("client_id", s.cfg.ThaIDClientID)
	data.Set("client_secret", s.cfg.ThaIDClientSecret)

	req, err := http.NewRequest(
		http.MethodPost,
		s.cfg.ThaIDRevokeURL,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return fmt.Errorf("thaid revoke request creation failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("thaid revoke request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("thaid revoke failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	return nil
}
