package config

import "os"

type Config struct {
	KeycloakLoginURL    string
	KeycloakTokenURL    string
	KeycloakLogoutURL   string
	KeycloakIssuer      string
	KeycloakRedirect    string
	KeycloakUserInfoURL string
	ClientID            string
	ClientSecret        string
	KeycloakPublicKey   string

	// ThaID (DOPA Digital ID) configuration
	ThaIDAuthURL      string
	ThaIDTokenURL     string
	ThaIDUserInfoURL  string
	ThaIDRevokeURL    string
	ThaIDClientID     string
	ThaIDClientSecret string
	ThaIDRedirectURI  string

	// FrontendURL is where the browser gets redirected after a successful
	// OAuth callback (must be browser-reachable, distinct from any internal proxy target).
	FrontendURL string
}

func Load() *Config {
	return &Config{
		KeycloakLoginURL:    os.Getenv("KEYCLOAK_LOGIN_URL"),
		KeycloakTokenURL:    os.Getenv("KEYCLOAK_TOKEN_URL"),
		KeycloakLogoutURL:   os.Getenv("KEYCLOAK_LOGOUT_URL"),
		KeycloakIssuer:      os.Getenv("KEYCLOAK_ISSUER"),
		KeycloakRedirect:    os.Getenv("KEYCLOAK_REDIRECT_URI"),
		KeycloakUserInfoURL: os.Getenv("KEYCLOAK_USERINFO_URL"),
		ClientID:            os.Getenv("KEYCLOAK_CLIENT_ID"),
		ClientSecret:        os.Getenv("KEYCLOAK_CLIENT_SECRET"),

		// ThaID
		ThaIDAuthURL:      os.Getenv("THAID_AUTH_URL"),
		ThaIDTokenURL:     os.Getenv("THAID_TOKEN_URL"),
		ThaIDUserInfoURL:  os.Getenv("THAID_USERINFO_URL"),
		ThaIDRevokeURL:    os.Getenv("THAID_REVOKE_URL"),
		ThaIDClientID:     os.Getenv("THAID_CLIENT_ID"),
		ThaIDClientSecret: os.Getenv("THAID_CLIENT_SECRET"),
		ThaIDRedirectURI:  os.Getenv("THAID_REDIRECT_URI"),

		FrontendURL: os.Getenv("FRONTEND_URL"),
	}
}
