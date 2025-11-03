package config

type Config struct {
	Address string `json:"address" env:"APP_ADDRESS"`
	Prefork bool   `json:"prefork" env:"APP_PREFORK"`
	Metrics *bool  `json:"metrics" env:"APP_METRICS"`

	Webp bool `json:"webp" env:"APP_WEBP"`

	AllowedOrigins []string `json:"allowedOrigins" env:"APP_ALLOWED_ORIGINS"`

	Token            string `json:"token" env:"APP_TOKEN"`
	HmacKey          string `json:"hmacKey" env:"APP_HMAC_KEY"`
	UploadingEnabled bool   `json:"uploadingEnabled" env:"APP_UPLOADING_ENABLED"`

	CacheTTL         int64 `json:"cacheTTLSeconds" env:"APP_CACHE_TTL_SECONDS"`
	CacheMaxCost     int64 `json:"cacheMaxCost" env:"APP_CACHE_MAX_COST"`
	CacheNumCounters int64 `json:"cacheNumCounters" env:"APP_CACHE_NUM_COUNTERS"`
	CacheBufferItems int64 `json:"cacheBufferItems" env:"APP_CACHE_BUFFER_ITEMS"`

	// Performance tuning options
	HTTPTimeout      int `json:"httpTimeoutSeconds" env:"APP_HTTP_TIMEOUT_SECONDS"`
	HTTPMaxIdleConns int `json:"httpMaxIdleConns" env:"APP_HTTP_MAX_IDLE_CONNS"`
	HTTPIdleTimeout  int `json:"httpIdleTimeoutSeconds" env:"APP_HTTP_IDLE_TIMEOUT_SECONDS"`
	HTTPCacheTTL     int `json:"httpCacheTTLSeconds" env:"APP_HTTP_CACHE_TTL_SECONDS"`
	MaxImageSize     int `json:"maxImageSizeMB" env:"APP_MAX_IMAGE_SIZE_MB"`
	MaxVideoSize     int `json:"maxVideoSizeMB" env:"APP_MAX_VIDEO_SIZE_MB"`
	URLCacheSize     int `json:"urlCacheSize" env:"APP_URL_CACHE_SIZE"`

	// Optional S3 storage for persistent result caching
	S3Enabled         bool   `json:"s3Enabled" env:"S3_ENABLED"`
	S3Endpoint        string `json:"s3Endpoint" env:"S3_ENDPOINT"`
	S3AccessKeyID     string `json:"s3AccessKeyId" env:"S3_ACCESS_KEY_ID"`
	S3SecretAccessKey string `json:"s3SecretAccessKey" env:"S3_SECRET_ACCESS_KEY"`
	S3Bucket          string `json:"s3Bucket" env:"S3_BUCKET"`
	S3SSL             bool   `json:"s3SSL" env:"S3_SSL"`
	S3Prefix          string `json:"s3Prefix" env:"S3_PREFIX"`
}
