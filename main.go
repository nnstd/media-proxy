package main

import (
	"log"
	"time"

	"github.com/gofiber/fiber/v2/middleware/cache"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"

	"go.uber.org/zap"

	"github.com/caarlos0/env/v11"
	"github.com/gofiber/fiber/v2"

	"media-proxy/config"
	"media-proxy/metrics"
	fiberprometheus "media-proxy/middlewares/prometheus"
	"media-proxy/routes"

	"github.com/dgraph-io/ristretto/v2"
)

var logger *zap.Logger

func main() {
	logger, _ = zap.NewProduction()
	defer func(logger *zap.Logger) {
		err := logger.Sync()
		if err != nil {
			log.Fatal(err)
		}
	}(logger)

	config, err := env.ParseAs[config.Config]()
	if err != nil {
		logger.Fatal(err.Error())
	}

	if config.Metrics == nil {
		metrics := true
		config.Metrics = &metrics
	}

	cacheConfig := &ristretto.Config[string, routes.CacheValue]{
		NumCounters: 1e7,     // number of keys to track frequency of (10M).
		MaxCost:     1 << 30, // maximum cost of cache (1GB).
		BufferItems: 64,      // number of keys per Get buffer.
	}

	if config.HTTPCacheTTL == 0 {
		config.HTTPCacheTTL = 1800 // 30 minutes
	}

	if config.CacheBufferItems > 0 {
		cacheConfig.BufferItems = config.CacheBufferItems
	}

	if config.CacheMaxCost > 0 {
		cacheConfig.MaxCost = config.CacheMaxCost
	}

	if config.CacheNumCounters > 0 {
		cacheConfig.NumCounters = config.CacheNumCounters
	}

	if config.CacheTTL == 0 {
		config.CacheTTL = 1800 // 30 minutes
	}

	cacheStore, err := ristretto.NewCache(cacheConfig)
	if err != nil {
		logger.Fatal(err.Error())
	}

	// Initialize optional S3 cache
	s3cache, s3err := routes.NewS3Cache(
		config.S3Enabled,
		config.S3Endpoint,
		config.S3AccessKeyID,
		config.S3SecretAccessKey,
		config.S3Bucket,
		config.S3SSL,
		config.S3Prefix,
	)
	if s3err != nil {
		logger.Warn("failed to initialize S3 cache", zap.Error(s3err))
	}

	// Initialize optional Redis upload tracker
	uploadTracker, redisErr := routes.NewRedisUploadTracker(
		config.RedisAddr,
		config.RedisPassword,
		config.RedisDB,
	)
	if redisErr != nil {
		logger.Warn("failed to initialize Redis upload tracker", zap.Error(redisErr))
	}
	if uploadTracker != nil {
		defer uploadTracker.Close()
	}

	// Configure body limit based on chunk size or max video size
	bodyLimit := 4 * 1024 * 1024 // Default 4MB
	if config.UploadingEnabled {
		if config.ChunkSize > 0 {
			bodyLimit = int(config.ChunkSize) + (1 * 1024 * 1024) // ChunkSize + 1MB buffer
		} else if config.MaxVideoSize > 0 {
			bodyLimit = config.MaxVideoSize * 1024 * 1024
		} else {
			bodyLimit = 100 * 1024 * 1024 // Default 100MB if no limits set
		}
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		Prefork:               config.Prefork,
		BodyLimit:             bodyLimit,
	})

	prometheusModule := fiberprometheus.New("media-proxy")
	prometheusModule.RegisterAt(app, "/metrics")

	prometheusRegistry := prometheusModule.GetRegistry()
	metrics := metrics.InitializeMetrics(prometheusRegistry, prometheusModule.GetConstLabels())

	if *config.Metrics {
		app.Use(prometheusModule.Middleware)
	}

	app.Use(healthcheck.New())
	app.Use(compress.New())
	app.Use(etag.New())
	app.Use(cache.New(cache.Config{
		Expiration: time.Minute * 10,
	}))

	routes.RegisterImageRoutes(logger, cacheStore, &config, app, metrics, s3cache)
	routes.RegisterVideoRoutes(logger, cacheStore, &config, app, metrics, s3cache, uploadTracker)

	address := config.Address
	if address == "" {
		address = ":3000"
	}

	log.Fatal(app.Listen(address))

	logger.Info("server started", zap.String("address", address))
}
