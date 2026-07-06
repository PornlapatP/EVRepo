package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/service"
)

// CitizenAuthMiddleware gates citizen-only routes (registration submission,
// "my registrations") on the app-issued "citizen_session" cookie set after a
// successful ThaID callback. This is intentionally separate from
// middleware.AuthMiddleware, which only verifies Keycloak-signed tokens and
// cannot verify DOPA's own tokens.
func CitizenAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionToken, err := c.Cookie("citizen_session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
			return
		}

		claims, err := service.ParseCitizenSession(sessionToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
			return
		}

		c.Set("pid", claims.PID)
		c.Set("citizen", claims)
		c.Next()
	}
}
