package middleware

import (
	"crypto/rsa"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pornlapatP/EV/internal/auth/service"
)

func AuthMiddleware(
	authService *service.AuthService,
	publicKey *rsa.PublicKey,
) gin.HandlerFunc {

	return func(c *gin.Context) {

		// 1️⃣ try access token
		accessToken, err := c.Cookie("access_token")
		if err == nil {

			token, err := VerifyAccessToken(accessToken, publicKey)
			if err == nil && token.Valid {
				c.Next()
				return
			}
		}

		// 2️⃣ try refresh token
		refreshToken, err := c.Cookie("refresh_token")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "unauthorized",
			})
			return
		}

		newToken, err := authService.Refresh(refreshToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "refresh token expired",
			})
			return
		}

		service.SetAuthCookies(c, newToken)

		c.Redirect(http.StatusFound, c.Request.RequestURI)

		c.Next()
	}
}
