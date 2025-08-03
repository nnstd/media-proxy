package config

type Config struct {
	Address string `json:"address" env:"APP_ADDRESS"`
	Prefork bool   `json:"prefork" env:"APP_PREFORK"`
	Metrics *bool  `json:"metrics" env:"APP_METRICS"`

	Webp bool `json:"webp" env:"APP_WEBP"`

	AllowedOrigins []string `json:"allowedOrigins" env:"APP_ALLOWED_ORIGINS"`

	Token   string `json:"token" env:"APP_TOKEN"`
	HmacKey string `json:"hmacKey" env:"APP_HMAC_KEY"`

	CacheTTL         int64 `json:"cacheTTLSeconds" env:"APP_CACHE_TTL_SECONDS"`
	CacheMaxCost     int64 `json:"cacheMaxCost" env:"APP_CACHE_MAX_COST"`
	CacheNumCounters int64 `json:"cacheNumCounters" env:"APP_CACHE_NUM_COUNTERS"`
	CacheBufferItems int64 `json:"cacheBufferItems" env:"APP_CACHE_BUFFER_ITEMS"`

	// Performance tuning options
	HTTPTimeout      int `json:"httpTimeoutSeconds" env:"APP_HTTP_TIMEOUT_SECONDS"`
	HTTPMaxIdleConns int `json:"httpMaxIdleConns" env:"APP_HTTP_MAX_IDLE_CONNS"`
	HTTPIdleTimeout  int `json:"httpIdleTimeoutSeconds" env:"APP_HTTP_IDLE_TIMEOUT_SECONDS"`
	MaxImageSize     int `json:"maxImageSizeMB" env:"APP_MAX_IMAGE_SIZE_MB"`
	MaxVideoSize     int `json:"maxVideoSizeMB" env:"APP_MAX_VIDEO_SIZE_MB"`
	URLCacheSize     int `json:"urlCacheSize" env:"APP_URL_CACHE_SIZE"`
}
