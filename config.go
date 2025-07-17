package main

type Config struct {
	Address string `json:"address" env:"APP_ADDRESS"`
	Prefork bool   `json:"prefork" env:"APP_PREFORK"`

	AllowedOrigins []string `json:"allowedOrigins" env:"APP_ALLOWED_ORIGINS"`
}
