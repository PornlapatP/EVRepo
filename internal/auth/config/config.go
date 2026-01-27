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
	}

}
