package config

type Config struct {
	Address string `json:"address" env:"APP_ADDRESS"`
	Prefork bool   `json:"prefork" env:"APP_PREFORK"`

	Webp bool `json:"webp" env:"APP_WEBP"`

	AllowedOrigins []string `json:"allowedOrigins" env:"APP_ALLOWED_ORIGINS"`
}
