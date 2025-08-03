package routes

import (
	"fmt"
	"io"
	"mime"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"media-proxy/client"
	"media-proxy/config"
	"media-proxy/metrics"
	"media-proxy/pool"
	"media-proxy/validation"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
)

// RegisterImageRoutes sets up image processing routes
func RegisterImageRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App, counters *metrics.Metrics) {
	// New path-based route: /images/q:50/w:500/h:300/webp/{base64-encoded-url}
	app.Get("/images/*", handleImageRequest(logger, cache, config, counters))

	// Legacy query-based route for backward compatibility
	app.Get("/image", handleImageRequestLegacy(logger, cache, config, counters))

	// Image upload route with path parameters
	app.Post("/images/upload/*", handleImageUpload(logger, cache, config, counters))

	// Legacy image upload route
	app.Post("/images", handleImageUploadLegacy(logger, cache, config, counters))
}

// handleImageRequest processes image requests with path parameters
func handleImageRequest(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pathParams := c.Params("*")
		logger.Info("image request received", zap.String("pathParams", pathParams))

		ok, status, err, params := validation.ProcessImageContextFromPath(logger, pathParams, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processImageResponse(c, logger, cache, config, counters, params)
	}
}

// handleImageRequestLegacy handles legacy query-based image requests
func handleImageRequestLegacy(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("image request received", zap.String("url", c.Query("url")))

		ok, status, err, params := validation.ProcessImageContext(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processImageResponse(c, logger, cache, config, counters, params)
	}
}

// processImageResponse handles the common image processing logic
func processImageResponse(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext) error {
	cacheKey := cacheKey(params.Url, params)
	cacheValue, ok := cache.Get(cacheKey)
	if ok {
		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
		counters.ServedCached.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		c.Set("Content-Type", cacheValue.ContentType)
		return c.Send(cacheValue.Body)
	}

	response, err := client.GetHTTPClient().Get(params.Url)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch image")
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			logger.Error("failed to close response body", zap.Error(closeErr))
		}
	}()

	responseContentType := response.Header.Get("Content-Type")
	if responseContentType == "" {
		return c.Status(fiber.StatusForbidden).SendString("no content type received")
	}

	parsedContentType, _, err := mime.ParseMediaType(responseContentType)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
	}

	if !validation.IsImageMime(parsedContentType) {
		return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
	}

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to read response body")
	}

	return processImageData(c, logger, cache, config, counters, params, responseBody, parsedContentType)
}

// processImageData handles the actual image processing and encoding
func processImageData(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext, imageData []byte, contentType string) error {
	cacheKey := cacheKey(params.Url, params)

	// Early return for unmodified images (no quality change, no webp, no resize, no scale)
	if params.Quality == 100 && !params.Webp && params.Width == 0 && params.Height == 0 && params.Scale == 0 {
		c.Set("Content-Type", contentType)
		cache.SetWithTTL(cacheKey, CacheValue{
			Body:        imageData,
			ContentType: contentType,
		}, 1000, time.Duration(config.CacheTTL)*time.Second)

		logger.Info("unmodified image served successfully", zap.String("content-type", contentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))
		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
		return c.Send(imageData)
	}

	// Process image only when modifications are needed
	img, err := readImageSlice(imageData, contentType)
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

	// Only encode to WebP if explicitly requested
	if params.Webp {
		c.Set("Content-Type", "image/webp")

		buf := pool.GetBuffer()
		defer pool.PutBuffer(buf)

		options, err := encoder.NewLossyEncoderOptions(encoder.PresetDefault, float32(params.Quality))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to create webp encoder options")
		}
		err = webp.Encode(buf, img, options)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to encode image")
		}

		cache.SetWithTTL(cacheKey, CacheValue{
			Body:        buf.Bytes(),
			ContentType: "image/webp",
		}, 1000, time.Duration(config.CacheTTL)*time.Second)

		logger.Info("image served successfully", zap.String("content-type", "image/webp"), zap.String("origin", params.Hostname), zap.String("url", params.Url))

		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		return c.Send(buf.Bytes())
	} else {
		// Use original format with quality adjustment
		c.Set("Content-Type", contentType)

		// For now, just return the processed image as the original format
		// TODO: Implement quality adjustment for other formats
		cache.SetWithTTL(cacheKey, CacheValue{
			Body:        imageData,
			ContentType: contentType,
		}, 1000, time.Duration(config.CacheTTL)*time.Second)

		logger.Info("image served successfully", zap.String("content-type", contentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		return c.Send(imageData)
	}
}

// handleImageUpload processes image upload requests with path parameters
func handleImageUpload(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("image upload request received")

		pathParams := c.Params("*")
		body, err := c.FormFile("image")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("failed to get image file")
		}

		imageFile, err := body.Open()
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("failed to open image file")
		}
		defer imageFile.Close()

		ok, status, err, params := validation.ProcessImageUploadFromPath(logger, pathParams, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		contentType := body.Header.Get("Content-Type")
		if contentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !validation.IsImageMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		requestBody, err := io.ReadAll(imageFile)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to read image file")
		}

		return processImageData(c, logger, cache, config, counters, params, requestBody, parsedContentType)
	}
}

// handleImageUploadLegacy handles legacy query-based image upload requests
func handleImageUploadLegacy(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("image upload request received")

		body, err := c.FormFile("image")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("failed to get image file")
		}

		imageFile, err := body.Open()
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("failed to open image file")
		}
		defer imageFile.Close()

		ok, status, err, params := validation.ProcessImageUpload(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		contentType := string(c.Request().Header.Peek("Content-Type"))
		if contentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !validation.IsImageMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		requestBody, err := io.ReadAll(imageFile)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to read image file")
		}

		return processImageData(c, logger, cache, config, counters, params, requestBody, parsedContentType)
	}
}
