package routes

import (
	"bytes"
	"fmt"
	"io"
	goMime "mime"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"media-proxy/config"
	"media-proxy/metrics"
	"media-proxy/mime"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
)

func RegisterImageRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App, counters *metrics.Metrics) {
	app.Get("/image", func(c *fiber.Ctx) error {
		logger.Info("image request received", zap.String("url", c.Query("url")))

		ok, status, err, params := processImageContext(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		cacheKey := cacheKey(params.Url, params)
		cacheValue, ok := cache.Get(cacheKey)
		if ok {
			counters.SuccessfullyServed.WithLabelValues("image", params.Hostname, params.Url).Inc()

			counters.ServedCached.WithLabelValues("image", params.Hostname, params.Url).Inc()

			c.Set("Content-Type", cacheValue.ContentType)
			return c.Send(cacheValue.Body)
		}

		response, err := http.Get(params.Url)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch image")
		}

		responseContentType := response.Header.Get("Content-Type")
		if responseContentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := goMime.ParseMediaType(responseContentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !mime.IsImageMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to read response body")
		}

		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				logger.Error(err.Error())
			}
		}(response.Body)

		if params.Quality != 100 || params.Webp {
			img, err := readImageSlice(responseBody, parsedContentType)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to read image")
			}

			if params.Width > 0 || params.Height > 0 {
				img, err = resizeImage(img, params.Width, params.Height, params.Interpolation)
				if err != nil {
					logger.Error("failed to resize image", zap.Error(err))
				}
			}

			if params.Scale > 0 {
				img, err = rescaleImage(img, params.Scale)
				if err != nil {
					logger.Error("failed to rescale image", zap.Error(err))
				}
			}

			c.Set("Content-Type", "image/webp")

			buf := bytes.NewBuffer(nil)
			err = webp.Encode(buf, img, &encoder.Options{
				Lossless: true,
				Quality:  float32(params.Quality),
			})
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to encode image")
			}

			cache.SetWithTTL(cacheKey, CacheValue{
				Body:        buf.Bytes(),
				ContentType: "image/webp",
			}, 1000, time.Duration(config.CacheTTL)*time.Second)

			logger.Info("image served successfully", zap.String("content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

			counters.SuccessfullyServed.WithLabelValues("image", params.Hostname, params.Url).Inc()

			return c.Send(buf.Bytes())
		} else {
			c.Set("Content-Type", parsedContentType)

			cache.SetWithTTL(cacheKey, CacheValue{
				Body:        responseBody,
				ContentType: parsedContentType,
			}, 1000, time.Duration(config.CacheTTL)*time.Second)

			logger.Info("image served successfully", zap.String("content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

			counters.SuccessfullyServed.WithLabelValues("image", params.Hostname, params.Url).Inc()

			return c.Send(responseBody)
		}
	})

	app.Post("/images", func(c *fiber.Ctx) error {
		logger.Info("image upload request received")

		body, err := c.FormFile("image")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("failed to get image file")
		}

		image, err := body.Open()
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("failed to open image file")
		}
		defer image.Close()

		ok, status, err, params := processImageUpload(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		cacheKey := cacheKey(params.Url, params)
		cacheValue, ok := cache.Get(cacheKey)
		if ok {
			counters.SuccessfullyServed.WithLabelValues("image", params.Hostname, params.Url).Inc()

			counters.ServedCached.WithLabelValues("image", params.Hostname, params.Url).Inc()

			c.Set("Content-Type", cacheValue.ContentType)
			return c.Send(cacheValue.Body)
		}

		contentType := string(c.Request().Header.Peek("Content-Type"))
		if contentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := goMime.ParseMediaType(contentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !mime.IsImageMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		requestBody, err := io.ReadAll(image)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to read image file")
		}

		if params.Quality != 100 || params.Webp {
			img, err := readImageSlice(requestBody, parsedContentType)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to read image")
			}

			if params.Width > 0 || params.Height > 0 {
				img, err = resizeImage(img, params.Width, params.Height, params.Interpolation)
				if err != nil {
					logger.Error("failed to resize image", zap.Error(err))
				}
			}

			if params.Scale > 0 {
				img, err = rescaleImage(img, params.Scale)
				if err != nil {
					logger.Error("failed to rescale image", zap.Error(err))
				}
			}

			c.Set("Content-Type", "image/webp")

			buf := bytes.NewBuffer(nil)
			err = webp.Encode(buf, img, &encoder.Options{
				Lossless: true,
				Quality:  float32(params.Quality),
			})
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to encode image")
			}

			cache.SetWithTTL(cacheKey, CacheValue{
				Body:        buf.Bytes(),
				ContentType: "image/webp",
			}, 1000, time.Duration(config.CacheTTL)*time.Second)

			logger.Info("image served successfully", zap.String("content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

			counters.SuccessfullyServed.WithLabelValues("image", params.Hostname, params.Url).Inc()

			return c.Send(buf.Bytes())
		} else {
			c.Set("Content-Type", parsedContentType)

			cache.SetWithTTL(cacheKey, CacheValue{
				Body:        requestBody,
				ContentType: parsedContentType,
			}, 1000, time.Duration(config.CacheTTL)*time.Second)

			logger.Info("image served successfully", zap.String("content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

			counters.SuccessfullyServed.WithLabelValues("image", params.Hostname, params.Url).Inc()

			return c.Send(requestBody)
		}
	})
}
