package main

import (
	"log"

	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"go.uber.org/zap"

	"github.com/caarlos0/env/v11"
	"github.com/gofiber/fiber/v2"

	"media-proxy/config"
	fiberprometheus "media-proxy/middlewares/prometheus"
	"media-proxy/metrics"
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

	cache, err := ristretto.NewCache(cacheConfig)
	if err != nil {
		logger.Fatal(err.Error())
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		Prefork:               config.Prefork,
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

	routes.RegisterImageRoutes(logger, cache, &config, app, metrics)
	routes.RegisterVideoRoutes(logger, cache, &config, app, metrics)

	address := config.Address
	if address == "" {
		address = ":3000"
	}

	log.Fatal(app.Listen(address))

	logger.Info("server started", zap.String("address", address))
}
