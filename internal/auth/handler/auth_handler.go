package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	// "github.com/pornlapatP/EV/internal/service"
	"github.com/pornlapatP/EV/internal/auth/service"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/models"
)

type AuthHandler struct {
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
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

	// 🔑 redirect กลับ frontend
	// c.JSON(200, gin.H{"message": "login success"})
	c.Redirect(http.StatusFound, "http://localhost:3000/api/profile")
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
		// log.Printf("User: %+v\n", user)

		c.JSON(200, gin.H{
			"id":                  user.Sub,
			"username":            user.PreferredUsername,
			"email":               user.Email,
			"name":                user.GivenName + " " + user.FamilyName,
			"hr_employee_id":      user.HrEmployeeId,
			"hr_fullname_th":      user.FamilyName,
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

	// c.Redirect(
	// 	http.StatusFound,
	// 	"http://localhost:3000/login",
	// )
}

func (h *AuthHandler) Register(c *gin.Context) {

	var req struct {
		Email     string `json:"email" binding:"required,email"`
		Username  string `json:"username" binding:"required"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	}
	// log.Printf("User: %+v\n", req)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	user := models.User{
		Email:     req.Email,
		Username:  req.Username,
		FirstName: req.FirstName,
		LastName:  req.LastName,
	}
	// log.Printf("User: %+v\n", user)
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(201, gin.H{"message": "registered"})
}
