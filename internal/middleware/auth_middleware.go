package middleware

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/service"
)

func AuthMiddleware(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {

		accessToken, err := c.Cookie("access_token")
		if err == nil {
			expired, err := IsExpired(accessToken)
			if err == nil && !expired {
				c.Next()
				return
			}
		}

		refreshToken, err := c.Cookie("refresh_token")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "missing refresh token",
			})
			return
		}

		newToken, err := authService.Refresh(refreshToken)
		log.Printf("Config: %+v\n", newToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "refresh token expired",
			})
			return
		}

		service.SetAuthCookies(c, newToken)

		c.Redirect(http.StatusFound, c.Request.RequestURI)
		c.Abort()
	}
}
