package middleware

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt"
	// "github.com/golang-jwt/jwt"
)

func IsExpired(accessToken string) (bool, error) {

	token, _, err := new(jwt.Parser).ParseUnverified(
		accessToken,
		jwt.MapClaims{},
	)
	if err != nil {
		return true, err
	}

	claims, ok := token.Claims.(jwt.MapClaims) // jwt.MapClaims = map[string]interface{}
	if !ok {
		return true, errors.New("invalid claims")
	}

	exp, ok := claims["exp"].(float64) // ดึงค่า exp
	if !ok {
		return true, errors.New("missing exp")
	}

	return time.Now().After(time.Unix(int64(exp), 0)), nil //แปลง exp เป็น time ถา้ now > exp หมดอายุ
}
