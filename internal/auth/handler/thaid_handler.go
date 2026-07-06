package handler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/config"
	"github.com/pornlapatP/EV/internal/auth/service"
)

// ThaIDHandler handles ThaID authentication HTTP requests.
type ThaIDHandler struct {
	thaidService *service.ThaIDService
	cfg          *config.Config
}

// NewThaIDHandler creates a new ThaIDHandler instance.
func NewThaIDHandler(thaidService *service.ThaIDService, cfg *config.Config) *ThaIDHandler {
	return &ThaIDHandler{
		thaidService: thaidService,
		cfg:          cfg,
	}
}

// Login redirects the user to ThaID authorization page.
func (h *ThaIDHandler) Login(c *gin.Context) {
	loginURL, state := h.thaidService.BuildLoginURL()

	// เก็บ state ใน cookie เพื่อตรวจสอบ CSRF ตอน callback
	c.SetCookie("thaid_state", state, 300, "/", "", false, true)

	c.Redirect(http.StatusFound, loginURL)
}

// Callback handles the ThaID authorization callback. On success it mints our
// own app-signed "citizen_session" cookie (see service.IssueCitizenSession) —
// downstream citizen routes are gated on that, not on DOPA's own token, since
// middleware.AuthMiddleware can only verify Keycloak-signed tokens.
func (h *ThaIDHandler) Callback(c *gin.Context) {
	// ตรวจสอบ error จาก ThaID
	if errMsg := c.Query("error"); errMsg != "" {
		errDesc := c.Query("error_description")
		log.Printf("ThaID auth error: %s - %s", errMsg, errDesc)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       errMsg,
			"description": errDesc,
		})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	// ตรวจสอบ state เพื่อป้องกัน CSRF
	state := c.Query("state")
	savedState, err := c.Cookie("thaid_state")
	if err != nil || state != savedState {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state parameter"})
		return
	}

	// ลบ state cookie หลังใช้งาน
	c.SetCookie("thaid_state", "", -1, "/", "", false, true)

	// แลก code เป็น token
	token, err := h.thaidService.ExchangeCode(code)
	if err != nil {
		log.Printf("ThaID token exchange error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange authorization code"})
		return
	}

	// ดึงข้อมูลผู้ใช้ (PID) ทันที เพื่อออก citizen session ของเราเอง
	user, err := h.thaidService.GetUserInfo(token.AccessToken)
	if err != nil {
		log.Printf("ThaID userinfo error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user info"})
		return
	}

	// Set cookies เหมือน Keycloak flow (ใช้ cookies ชุดเดียวกัน) — ยังเก็บไว้เพื่อใช้ตอน logout/revoke
	service.SetAuthCookies(c, &service.TokenResponse{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    token.ExpiresIn,
	})
	c.SetCookie("auth_provider", "thaid", token.ExpiresIn, "/", "", false, true)

	citizenSession, err := service.IssueCitizenSession(service.CitizenClaims{
		PID:       user.PID,
		FirstName: user.GivenName,
		LastName:  user.FamilyName,
		Address:   user.Address,
	})
	if err != nil {
		log.Printf("issue citizen session error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start session"})
		return
	}
	c.SetCookie("citizen_session", citizenSession, 24*60*60, "/", "", false, true)

	c.Redirect(http.StatusFound, fmt.Sprintf("%s/registrationForm", h.cfg.FrontendURL))
}

// Logout handles ThaID logout by revoking the token.
func (h *ThaIDHandler) Logout(c *gin.Context) {
	accessToken, err := c.Cookie("access_token")
	if err == nil {
		if revokeErr := h.thaidService.Revoke(accessToken); revokeErr != nil {
			log.Printf("ThaID revoke error: %v", revokeErr)
		}
	}

	service.ClearAuthCookies(c)
	c.SetCookie("auth_provider", "", -1, "/", "", false, true)
	c.SetCookie("citizen_session", "", -1, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"message": "logged out from ThaID",
	})
}

// ThaIDProfileHandler returns user info from ThaID userinfo endpoint.
func ThaIDProfileHandler(thaidService *service.ThaIDService) gin.HandlerFunc {
	return func(c *gin.Context) {
		accessToken, err := c.Cookie("access_token")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing access token"})
			return
		}

		user, err := thaidService.GetUserInfo(accessToken)
		if err != nil {
			log.Printf("ThaID userinfo error: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "failed to get user info"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"provider":         "thaid",
			"sub":              user.Sub,
			"pid":              user.PID,
			"name":             user.Name,
			"given_name":       user.GivenName,
			"middle_name":      user.MiddleName,
			"family_name":      user.FamilyName,
			"name_en":          user.NameEN,
			"given_name_en":    user.GivenNameEN,
			"middle_name_en":   user.MiddleNameEN,
			"family_name_en":   user.FamilyNameEN,
			"title":            user.Title,
			"title_en":         user.TitleEN,
			"birthdate":        user.Birthdate,
			"gender":           user.Gender,
			"address":          user.Address,
			"ial":              user.IAL,
			"smartcard_code":   user.SmartcardCode,
			"date_of_expiry":   user.DateOfExpiry,
			"date_of_issuance": user.DateOfIssuance,
		})
	}
}
