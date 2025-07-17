package main

type Config struct {
	AllowedOrigins []string `json:"allowedOrigins" env:"APP_ALLOWED_ORIGINS"`
}
