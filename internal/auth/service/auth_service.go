package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/config"
)

type AuthService struct {
	cfg *config.Config
}

func NewAuthService(cfg *config.Config) *AuthService {
	return &AuthService{
		cfg: cfg,
	}
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *AuthService) ExchangeCode(code string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", s.cfg.ClientID)
	data.Set("client_secret", s.cfg.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", s.cfg.KeycloakRedirect)

	resp, err := http.PostForm(s.cfg.KeycloakTokenURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}

	return &token, nil
}

func (s *AuthService) BuildLoginURL() string {
	// log.Printf("AuthService: %+v\n", s)
	// log.Printf("Config: %+v\n", s.cfg)

	return fmt.Sprintf(
		"%s?client_id=%s&response_type=code&scope=openid+email+profile&redirect_uri=%s",
		s.cfg.KeycloakLoginURL,
		s.cfg.ClientID,
		url.QueryEscape(s.cfg.KeycloakRedirect),
	)
}

type KeycloakUser struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	HrEmployeeId      string `json:"hr_employee_id"`
	HrFullNameTh      string `json:"hr_fullname_th"`
	HrDepartment      string `json:"hr_department"`
	HrPosition        string `json:"hr_position"`
	HrDeptChangeCode  string `json:"hr_dept_change_code"`
	HrCostCenter      string `json:"hr_cost_center"`
	HrDeptSap         string `json:"hr_dept_sap"`
}

func (s *AuthService) GetUserInfo(accessToken string) (*KeycloakUser, error) {
	req, err := http.NewRequest(
		http.MethodGet,
		s.cfg.KeycloakUserInfoURL,
		nil, // no body
	)
	if err != nil {
		return nil, err
	}
	// log.Printf("REQ: %+v\n", req)
	req.Header.Set("Authorization", "Bearer "+accessToken) // key cloak ต้องการ Bearer

	client := &http.Client{Timeout: 5 * time.Second} // รอ 5 วิ
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //ปิด body กัน memory / fd leak

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("access token expired or invalid") // check token
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf(
			"userinfo failed: status=%d body=%s",
			resp.StatusCode,
			string(body),
		)
	}

	var user KeycloakUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil { //change json to stuck
		return nil, err
	}

	return &user, nil
}

func (s *AuthService) Refresh(refreshToken string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", s.cfg.ClientID)
	data.Set("client_secret", s.cfg.ClientSecret)
	data.Set("refresh_token", refreshToken)

	resp, err := http.PostForm(s.cfg.KeycloakTokenURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("refresh token failed")
	}

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}

	return &token, nil
}
func SetAuthCookies(c *gin.Context, token *TokenResponse) {

	// log.Printf("Config: %+v\n", token)

	c.SetCookie(
		"access_token",
		token.AccessToken,
		int(token.ExpiresIn),
		"/",
		"",
		false,
		true,
	)

	c.SetCookie(
		"refresh_token",
		token.RefreshToken,
		7*24*60*60,
		"/",
		"",
		false,
		true,
	)
}

func (s *AuthService) Logout(refreshToken string) error {
	data := url.Values{}
	data.Set("client_id", s.cfg.ClientID)
	data.Set("client_secret", s.cfg.ClientSecret)
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequest(
		http.MethodPost,
		s.cfg.KeycloakLogoutURL,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("keycloak logout failed: %s", body)
	}

	return nil
}
