package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

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

// Login redirects the user to ThaID authorization page. A caller (e.g. a
// Smart Plus deep-link) can mark itself with ?source=smartplus; anything else
// (including a direct ThaID login) is treated as EntrySourceThaID — the
// more restrictive, view-only default (see CitizenClaims.EntrySource).
// DOPA's callback only echoes back "code" and "state", so the source rides
// along inside the thaid_state cookie value rather than as its own param.
func (h *ThaIDHandler) Login(c *gin.Context) {
	loginURL, state := h.thaidService.BuildLoginURL()
	source := service.ParseEntrySource(c.Query("source"))

	// เก็บ state+source ใน cookie เพื่อตรวจสอบ CSRF ตอน callback
	c.SetCookie("thaid_state", state+"."+string(source), 300, "/", "", service.SecureCookie(), true)

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

	// ตรวจสอบ state เพื่อป้องกัน CSRF — cookie เก็บเป็น "state.source" (ดู Login)
	state := c.Query("state")
	savedStateAndSource, err := c.Cookie("thaid_state")
	savedState, source, _ := strings.Cut(savedStateAndSource, ".")
	if err != nil || state == "" || state != savedState {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state parameter"})
		return
	}
	entrySource := service.ParseEntrySource(source)

	// ลบ state cookie หลังใช้งาน
	c.SetCookie("thaid_state", "", -1, "/", "", service.SecureCookie(), true)

	// แลก code เป็น token
	token, err := h.thaidService.ExchangeCode(code)
	if err != nil {
		log.Printf("ThaID token exchange error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange authorization code"})
		return
	}

	// ไม่มี userinfo endpoint แยกใน DOPA sandbox — pid/name/address ฯลฯ มากับ token
	// response ชุดนี้เลย (manual §6.2.2) จึงออก citizen session จาก token ตรงๆ
	if token.PID == "" {
		log.Printf("ThaID token response missing pid (scope=%s)", token.Scope)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user info"})
		return
	}

	// Set cookies เหมือน Keycloak flow (ใช้ cookies ชุดเดียวกัน) — ยังเก็บไว้เพื่อใช้ตอน logout/revoke
	service.SetAuthCookies(c, &service.TokenResponse{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    token.ExpiresIn,
	})
	c.SetCookie("auth_provider", "thaid", token.ExpiresIn, "/", "", service.SecureCookie(), true)

	citizenSession, err := service.IssueCitizenSession(service.CitizenClaims{
		PID:         token.PID,
		FirstName:   token.GivenName,
		LastName:    token.FamilyName,
		Address:     string(token.Address),
		EntrySource: entrySource,
	})
	if err != nil {
		log.Printf("issue citizen session error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start session"})
		return
	}
	c.SetCookie("citizen_session", citizenSession, 24*60*60, "/", "", service.SecureCookie(), true)

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
	c.SetCookie("auth_provider", "", -1, "/", "", service.SecureCookie(), true)
	c.SetCookie("citizen_session", "", -1, "/", "", service.SecureCookie(), true)

	c.JSON(http.StatusOK, gin.H{
		"message": "logged out from ThaID",
	})
}

// ThaIDProfileHandler returns the identity carried in the app's own
// "citizen_session" cookie. There's no DOPA userinfo endpoint to re-query
// (see Callback) so this reflects whatever was captured from the token
// response at login time, not a live ThaID lookup.
func ThaIDProfileHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionCookie, err := c.Cookie("citizen_session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing citizen session"})
			return
		}

		claims, err := service.ParseCitizenSession(sessionCookie)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid citizen session"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"provider":    "thaid",
			"pid":         claims.PID,
			"firstName":   claims.FirstName,
			"lastName":    claims.LastName,
			"address":     claims.Address,
			"entrySource": claims.EntrySource,
		})
	}
}
