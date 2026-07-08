package service

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt"
)

const citizenSessionTTL = 24 * time.Hour

// EntrySource identifies which app launched the ThaID login, since editing
// rights differ by channel (design/03-role-matrix.md §PLANNED): Smart Plus
// can edit an existing registration, a direct ThaID login can only view one.
type EntrySource string

const (
	EntrySourceSmartPlus EntrySource = "smartplus"
	EntrySourceThaID     EntrySource = "thaid"
)

// ParseEntrySource normalizes an untrusted "source" query value — anything
// other than the recognized Smart Plus value is treated as a direct ThaID
// login (the more restrictive, view-only default).
func ParseEntrySource(raw string) EntrySource {
	if EntrySource(raw) == EntrySourceSmartPlus {
		return EntrySourceSmartPlus
	}
	return EntrySourceThaID
}

// CitizenClaims is the identity carried in the app-issued citizen session —
// sourced once from ThaID's userinfo at callback time, so the registration
// wizard never has to (re-)collect name/address itself.
type CitizenClaims struct {
	PID         string
	FirstName   string
	LastName    string
	Address     string
	EntrySource EntrySource
}

// citizenSessionSecret returns the HMAC secret used to sign/verify the app's
// own citizen session token. This is a separate trust domain from Keycloak's
// RSA key (KEYCLOAK_PUBLIC_KEY) — we are the issuer here, not verifying a
// third party's token.
func citizenSessionSecret() ([]byte, error) {
	secret := os.Getenv("CITIZEN_SESSION_SECRET")
	if secret == "" {
		return nil, errors.New("CITIZEN_SESSION_SECRET missing")
	}
	return []byte(secret), nil
}

// IssueCitizenSession mints a short-lived app-signed token identifying a
// citizen by ThaID PID (plus their ThaID name/address), to be stored in the
// "citizen_session" cookie.
func IssueCitizenSession(claims CitizenClaims) (string, error) {
	secret, err := citizenSessionSecret()
	if err != nil {
		return "", err
	}

	jwtClaims := jwt.MapClaims{
		"pid":         claims.PID,
		"firstName":   claims.FirstName,
		"lastName":    claims.LastName,
		"address":     claims.Address,
		"entrySource": string(claims.EntrySource),
		"iat":         time.Now().Unix(),
		"exp":         time.Now().Add(citizenSessionTTL).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)
	return token.SignedString(secret)
}

// ParseCitizenSession verifies a citizen session token and returns the
// identity it was issued for.
func ParseCitizenSession(tokenString string) (CitizenClaims, error) {
	secret, err := citizenSessionSecret()
	if err != nil {
		return CitizenClaims{}, err
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return CitizenClaims{}, errors.New("invalid citizen session")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return CitizenClaims{}, errors.New("invalid citizen session claims")
	}

	pid, _ := claims["pid"].(string)
	if pid == "" {
		return CitizenClaims{}, errors.New("citizen session missing pid")
	}

	firstName, _ := claims["firstName"].(string)
	lastName, _ := claims["lastName"].(string)
	address, _ := claims["address"].(string)
	entrySource, _ := claims["entrySource"].(string)

	return CitizenClaims{
		PID:         pid,
		FirstName:   firstName,
		LastName:    lastName,
		Address:     address,
		EntrySource: ParseEntrySource(entrySource),
	}, nil
}
