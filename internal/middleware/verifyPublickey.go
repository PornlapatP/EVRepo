package middleware

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt"
)

func VerifyAccessToken(tokenString string, key *rsa.PublicKey) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {

		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected signing method")
		}

		return key, nil
	})
}

func ParseRSAPublicKey(raw string) (*rsa.PublicKey, error) {
	if raw == "" {
		return nil, errors.New("empty public key")
	}
	// log.Printf("Config: %+v\n", raw)
	// 🔥 สำคัญ: ลบ whitespace / newline
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\n", "")
	raw = strings.ReplaceAll(raw, " ", "")

	pemKey := fmt.Sprintf(
		"-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----",
		raw,
	)

	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not RSA public key")
	}

	return rsaPub, nil
}
