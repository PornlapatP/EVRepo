package service

import (
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// SecureCookie reports whether auth cookies should get the `Secure` flag.
//
// Default follows the stage name (`ENV=production`), but the flag really
// depends on the *scheme the deployment is served over*, not on what the stage
// is called: a dev/staging box behind HTTPS must still mark its cookies Secure
// (otherwise they leak over any plain-http request to the same host, and a MITM
// can overwrite them), while a production build exercised locally over http
// must not. `COOKIE_SECURE=true|false` overrides the stage for exactly those
// cases; unset keeps the old behaviour.
func SecureCookie() bool {
	if v, err := strconv.ParseBool(os.Getenv("COOKIE_SECURE")); err == nil {
		return v
	}
	return os.Getenv("ENV") == "production"
}

func ClearAuthCookies(c *gin.Context) {
	c.SetCookie("access_token", "", -1, "/", "", SecureCookie(), true)
	c.SetCookie("refresh_token", "", -1, "/", "", SecureCookie(), true)
}
