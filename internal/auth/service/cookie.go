package service

import (
	"os"

	"github.com/gin-gonic/gin"
)

// SecureCookie reports whether auth cookies should get the `Secure` flag —
// true in production (served over HTTPS), false for local http dev.
func SecureCookie() bool {
	return os.Getenv("ENV") == "production"
}

func ClearAuthCookies(c *gin.Context) {
	c.SetCookie("access_token", "", -1, "/", "", SecureCookie(), true)
	c.SetCookie("refresh_token", "", -1, "/", "", SecureCookie(), true)
}
