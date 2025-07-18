package main

import (
	"log"

	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"go.uber.org/zap"

	"github.com/caarlos0/env/v11"
	"github.com/gofiber/fiber/v2"

	"media-proxy/config"
	"media-proxy/routes"
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

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		Prefork:               config.Prefork,
	})

	app.Use(healthcheck.New())
	app.Use(compress.New())

	routes.RegisterImageRoutes(logger, &config, app)
	routes.RegisterVideoRoutes(logger, &config, app)

	address := config.Address
	if address == "" {
		address = ":3000"
	}

	log.Fatal(app.Listen(address))

	logger.Info("server started", zap.String("address", address))
}
