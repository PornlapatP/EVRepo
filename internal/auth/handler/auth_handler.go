package handler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/config"
	"github.com/pornlapatP/EV/internal/auth/service"
	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm"
)

type AuthHandler struct {
	authService *service.AuthService
	cfg         *config.Config
	db          *gorm.DB
}

func NewAuthHandler(authService *service.AuthService, cfg *config.Config, db *gorm.DB) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		cfg:         cfg,
		db:          db,
	}
}
func (h *AuthHandler) Login(c *gin.Context) {
	loginURL := h.authService.BuildLoginURL()
	c.Redirect(http.StatusFound, loginURL)
}

func (h *AuthHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		c.JSON(400, gin.H{"error": "missing code"})
		return
	}

	token, err := h.authService.ExchangeCode(code)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	service.SetAuthCookies(c, token)

	if user, err := h.authService.GetUserInfo(token.AccessToken); err == nil {
		employee := models.Employee{
			Sub:          user.Sub,
			Email:        user.Email,
			GivenName:    user.GivenName,
			FamilyName:   user.FamilyName,
			HrEmployeeId: user.HrEmployeeId,
			HrDepartment: user.HrDepartment,
			HrPosition:   user.HrPosition,
		}
		if err := h.db.Where(models.Employee{Sub: user.Sub}).Assign(employee).FirstOrCreate(&employee).Error; err != nil {
			log.Printf("upsert employee error: %v", err)
		}
	} else {
		log.Printf("keycloak userinfo error: %v", err)
	}

	// redirect กลับ frontend's back-office console — this is the only page
	// Keycloak login exists for (staff review, see internal/admin); citizens
	// never hit this route (they use /api/auth/thaid/login instead).
	c.Redirect(http.StatusFound, fmt.Sprintf("%s/backoffice", h.cfg.FrontendURL))
}

func ProfileHandler(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		accessToken, err := c.Cookie("access_token")
		if err != nil {
			c.AbortWithStatus(401)
			return
		}

		user, err := authService.GetUserInfo(accessToken)
		if err != nil {
			c.AbortWithStatus(401)
			return
		}
		log.Printf("User: %+v\n", user)

		c.JSON(200, gin.H{
			"id":                  user.Sub,
			"username":            user.PreferredUsername,
			"email":               user.Email,
			"name":                user.GivenName + " " + user.FamilyName,
			"hr_employee_id":      user.HrEmployeeId,
			"hr_fullname_th":      user.HrFullNameTh,
			"hr_department":       user.HrDepartment,
			"hr_position":         user.HrPosition,
			"hr_dept_change_code": user.HrDeptChangeCode,
			"hr_cost_center":      user.HrCostCenter,
			"hr_dept_sap":         user.HrDeptSap,
		})
	}
}

func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err == nil {
		_ = h.authService.Logout(refreshToken) //keycloak
	}

	service.ClearAuthCookies(c) // Delete cookie

	c.JSON(200, gin.H{
		"message": "logged out",
	})
}
