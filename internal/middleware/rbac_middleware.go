package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/service"
	"github.com/pornlapatP/EV/internal/rbac"
	"gorm.io/gorm"
)

// RBACMiddleware gates a back-office route to callers whose department
// (hr_dept_change_code from Keycloak) has been granted the given
// resource/action in RulePolicy. Must run after AuthMiddleware, which has
// already verified the access token is valid.
func RBACMiddleware(db *gorm.DB, authService *service.AuthService, resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		accessToken, err := c.Cookie("access_token")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
			return
		}

		user, err := authService.GetUserInfo(accessToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
			return
		}

		role, err := rbac.ResolveRole(db, user.HrDeptChangeCode)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "NO_ROLE",
				"message": "บัญชีนี้ไม่มีสิทธิ์เข้าใช้งานระบบ back-office",
			})
			return
		}

		if !rbac.HasPermission(db, role.ID, resource, action) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "FORBIDDEN",
				"message": "คุณไม่มีสิทธิ์ทำรายการนี้",
			})
			return
		}

		c.Set("rbac_role", role.RoleName)
		c.Next()
	}
}
