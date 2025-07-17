package main

import (
	"fmt"
	"image"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"

	"github.com/asticode/go-astiav"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
	"go.uber.org/zap"

	"github.com/caarlos0/env/v11"
	"github.com/gofiber/fiber/v2"
)

var logger *zap.Logger

func validateUrl(urlStr string, origins []string) (valid bool, hostname string) {
	parsedUrl, err := url.Parse(urlStr)
	if err != nil {
		return false, ""
	}

	if len(origins) == 0 {
		return true, ""
	}

	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return false, ""
	}

	for _, origin := range origins {
		hostname = parsedUrl.Hostname()

		logger.Debug("matching origin", zap.String("origin", origin), zap.String("hostname", hostname))

		if origin == hostname {
			logger.Debug("origin matched", zap.String("origin", origin), zap.String("hostname", hostname))
			return true, hostname
		}
	}

	return false, ""
}

func frameToImage(frame *astiav.Frame) (image.Image, error) {
	img, err := frame.Data().GuessImageFormat()
	if err != nil {
		return nil, fmt.Errorf("failed to guess image format: %w", err)
	}

	err = frame.Data().ToImage(img)
	if err != nil {
		return nil, fmt.Errorf("failed to convert frame to image: %w", err)
	}

	return img, nil
}

func main() {
	logger, _ = zap.NewProduction()
	defer func(logger *zap.Logger) {
		err := logger.Sync()
		if err != nil {
			log.Fatal(err)
		}
	}(logger)

	config, err := env.ParseAs[Config]()
	if err != nil {
		logger.Fatal(err.Error())
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(healthcheck.New())
	app.Use(compress.New())

	//#region Image Proxy

	app.Get("/image", func(c *fiber.Ctx) error {
		logger.Info("image request received", zap.String("url", c.Query("url")))

		urlParam := c.Query("url")
		if urlParam == "" {
			return c.Status(fiber.StatusBadRequest).SendString("url is required")
		}

		quality := c.QueryInt("quality", 100)
		if quality < 1 || quality > 100 {
			return c.Status(fiber.StatusBadRequest).SendString("quality must be between 1 and 100")
		}

		forceWebp := c.QueryBool("webp", false)

		validOrigin, hostname := validateUrl(urlParam, config.AllowedOrigins)
		if !validOrigin {
			return c.Status(fiber.StatusForbidden).SendString("url is not allowed")
		}

		response, err := http.Get(urlParam)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch image")
		}

		responseContentType := response.Header.Get("Content-Type")
		if responseContentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := mime.ParseMediaType(responseContentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !isImageMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				logger.Error(err.Error())
			}
		}(response.Body)

		if quality != 100 || forceWebp {
			img, err := readImage(response.Body, parsedContentType)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to read image")
			}

			c.Set("Content-Type", "image/webp")

			webp.Encode(c, img, &encoder.Options{
				Lossless: true,
				Quality:  float32(quality),
			})
		} else {
			c.Set("Content-Type", parsedContentType)

			_, err = io.Copy(c, response.Body)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to copy image")
			}
		}

		logger.Info("image served successfully", zap.String("content-type", parsedContentType), zap.String("origin", hostname), zap.String("url", urlParam))

		return c.SendStatus(fiber.StatusOK)
	})

	//#endregion

	//#region Video Proxy

	app.Get("/video/preview", func(c *fiber.Ctx) error {
		logger.Info("video preview request received", zap.String("url", c.Query("url")))

		urlParam := c.Query("url")
		if urlParam == "" {
			return c.Status(fiber.StatusBadRequest).SendString("url is required")
		}

		validOrigin, hostname := validateUrl(urlParam, config.AllowedOrigins)
		if !validOrigin {
			return c.Status(fiber.StatusForbidden).SendString("url is not allowed")
		}

		// First check if it's a video by doing a HEAD request
		headResp, err := http.Get(urlParam)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to check video")
		}
		headResp.Body.Close()

		responseContentType := headResp.Header.Get("Content-Type")
		if responseContentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := mime.ParseMediaType(responseContentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !isVideoMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		// Extract first frame using streaming approach
		frameData, err := extractFirstFrame(urlParam)
		if err != nil {
			logger.Error("failed to extract first frame", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to extract video preview")
		}

		// Set response headers
		c.Set("Content-Type", "image/jpeg")
		c.Set("Cache-Control", "public, max-age=3600")

		logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", hostname), zap.String("url", urlParam))

		return c.Send(frameData)
	})
	
	//#endregion

	log.Fatal(app.Listen(":3000"))
}
