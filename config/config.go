package config

type Config struct {
	Address string `json:"address" env:"APP_ADDRESS"`
	Prefork bool   `json:"prefork" env:"APP_PREFORK"`

	Webp bool `json:"webp" env:"APP_WEBP"`

	AllowedOrigins []string `json:"allowedOrigins" env:"APP_ALLOWED_ORIGINS"`

	HmacKey string `json:"hmacKey" env:"APP_HMAC_KEY"`

	CacheTTL         int64 `json:"cacheTTLSeconds" env:"APP_CACHE_TTL_SECONDS"`
	CacheMaxCost     int64 `json:"cacheMaxCost" env:"APP_CACHE_MAX_COST"`
	CacheNumCounters int64 `json:"cacheNumCounters" env:"APP_CACHE_NUM_COUNTERS"`
	CacheBufferItems int64 `json:"cacheBufferItems" env:"APP_CACHE_BUFFER_ITEMS"`
}
