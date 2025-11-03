package routes

import (
	"context"
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
func RegisterImageRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App, counters *metrics.Metrics, s3cache *S3Cache) {
	// New path-based route: /images/q:50/w:500/h:300/webp/{base64-encoded-url}
	app.Get("/images/*", handleImageRequest(logger, cache, config, counters, s3cache))

	// Image upload route with path parameters
	app.Post("/images/upload/*", handleImageUpload(logger, cache, config, counters, s3cache))

	// Legacy image upload route
	app.Post("/images", handleImageUploadLegacy(logger, cache, config, counters, s3cache))
}

//#region handleImageRequest

// handleImageRequest processes image requests with path parameters
func handleImageRequest(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pathParams := c.Params("*")
		logger.Info("image request received", zap.String("pathParams", pathParams))

		ok, status, err, params := validation.ProcessImageContextFromPath(logger, pathParams, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processImageResponse(c, logger, cache, config, counters, params, s3cache)
	}
}

//#endregion

//#region processImageResponse

// processImageResponse handles the common image processing logic
func processImageResponse(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext, s3cache *S3Cache) error {
	cacheKey := cacheKey(params.Url, params)
	cacheValue, ok := cache.Get(cacheKey)
	if ok {
		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
		counters.ServedCached.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		c.Set("Content-Type", cacheValue.ContentType)
		return c.Send(cacheValue.Body)
	}

	// Try S3 cache if enabled
	if s3cache != nil && s3cache.Enabled {
		if params.CustomObjectKey != "" {
			if s3val, err := s3cache.GetAtLocation(context.Background(), params.CustomObjectKey); err == nil && s3val != nil {
				counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
				counters.ServedCached.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
				c.Set("Content-Type", s3val.ContentType)
				return c.Send(s3val.Body)
			}
		}
		if s3val, err := s3cache.Get(context.Background(), cacheKey); err == nil && s3val != nil {
			counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
			counters.ServedCached.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
			c.Set("Content-Type", s3val.ContentType)
			// backfill in-memory cache
			cache.SetWithTTL(cacheKey, *s3val, 1000, time.Duration(config.CacheTTL)*time.Second)
			return c.Send(s3val.Body)
		}
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

	return processImageData(c, logger, cache, config, counters, params, responseBody, parsedContentType, s3cache)
}

//#endregion

//#region processImageData

// processImageData handles the actual image processing and encoding
func processImageData(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext, imageData []byte, contentType string, s3cache *S3Cache) error {
	cacheKey := cacheKey(params.Url, params)

	// Early return for unmodified images (no quality change, no webp, no resize, no scale)
	if params.Quality == 100 && !params.Webp && params.Width == 0 && params.Height == 0 && params.Scale == 0 {
		c.Set("Content-Type", contentType)
		c.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", config.HTTPCacheTTL))

		value := CacheValue{
			Body:        imageData,
			ContentType: contentType,
		}
		cache.SetWithTTL(cacheKey, value, 1000, time.Duration(config.CacheTTL)*time.Second)
		// store in S3 asynchronously
		if s3cache != nil && s3cache.Enabled {
			if params.CustomObjectKey != "" {
				// store at explicit location
				go func() {
					_ = s3cache.PutAtLocation(context.Background(), params.CustomObjectKey, value.Body, value.ContentType)
				}()
			} else {
				go func() { _ = s3cache.Put(context.Background(), cacheKey, value.Body, value.ContentType) }()
			}
		}

		logger.Debug("unmodified image served successfully", zap.String("content-type", contentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))
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
		c.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", config.HTTPCacheTTL))

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

		value := CacheValue{Body: buf.Bytes(), ContentType: "image/webp"}
		cache.SetWithTTL(cacheKey, value, 1000, time.Duration(config.CacheTTL)*time.Second)
		if s3cache != nil && s3cache.Enabled {
			data := make([]byte, len(value.Body))
			copy(data, value.Body)
			if params.CustomObjectKey != "" {
				go func() {
					_ = s3cache.PutAtLocation(context.Background(), params.CustomObjectKey, data, value.ContentType)
				}()
			} else {
				go func() { _ = s3cache.Put(context.Background(), cacheKey, data, value.ContentType) }()
			}
		}

		logger.Info("image served successfully", zap.String("content-type", "image/webp"), zap.String("origin", params.Hostname), zap.String("url", params.Url))

		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		return c.Send(buf.Bytes())
	} else {
		// Use original format with quality adjustment
		c.Set("Content-Type", contentType)
		c.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", config.HTTPCacheTTL))

		// For now, just return the processed image as the original format
		// TODO: Implement quality adjustment for other formats
		value := CacheValue{Body: imageData, ContentType: contentType}
		cache.SetWithTTL(cacheKey, value, 1000, time.Duration(config.CacheTTL)*time.Second)
		if s3cache != nil && s3cache.Enabled {
			data := make([]byte, len(value.Body))
			copy(data, value.Body)
			if params.CustomObjectKey != "" {
				go func() {
					_ = s3cache.PutAtLocation(context.Background(), params.CustomObjectKey, data, value.ContentType)
				}()
			} else {
				go func() { _ = s3cache.Put(context.Background(), cacheKey, data, value.ContentType) }()
			}
		}

		logger.Info("image served successfully", zap.String("content-type", contentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

		counters.SuccessfullyServed.WithLabelValues("image", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		return c.Send(imageData)
	}
}

//#endregion

//#region handleImageUpload

// handleImageUpload processes image upload requests with path parameters
func handleImageUpload(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
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

		return processImageData(c, logger, cache, config, counters, params, requestBody, parsedContentType, s3cache)
	}
}

//#endregion

//#region handleImageUploadLegacy

// handleImageUploadLegacy handles legacy query-based image upload requests
func handleImageUploadLegacy(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
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

		return processImageData(c, logger, cache, config, counters, params, requestBody, parsedContentType, s3cache)
	}
}

//#endregion
