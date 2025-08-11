package routes

import (
	"fmt"
	"mime"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"image/jpeg"
	"media-proxy/config"
	"media-proxy/metrics"
	"media-proxy/pool"
	"media-proxy/validation"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
)

// RegisterVideoRoutes sets up video processing routes
func RegisterVideoRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App, counters *metrics.Metrics) {
	// New path-based route: /videos/preview/q:50/w:500/h:300/webp/{base64-encoded-url}
	app.Get("/videos/preview/*", handleVideoPreviewRequest(logger, cache, config, counters))

	// Legacy query-based route for backward compatibility
	app.Get("/video/preview", handleVideoPreviewRequestLegacy(logger, cache, config, counters))
}

// handleVideoPreviewRequest processes video preview requests with path parameters
func handleVideoPreviewRequest(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pathParams := c.Params("*")
		logger.Info("video preview request received", zap.String("pathParams", pathParams))

		ok, status, err, params := validation.ProcessImageContextFromPath(logger, pathParams, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processVideoPreview(c, logger, cache, config, counters, params)
	}
}

// handleVideoPreviewRequestLegacy handles legacy query-based video preview requests
func handleVideoPreviewRequestLegacy(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("video preview request received", zap.String("url", c.Query("url")))

		ok, status, err, params := validation.ProcessImageContext(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processVideoPreview(c, logger, cache, config, counters, params)
	}
}

// processVideoPreview handles the common video preview processing logic
func processVideoPreview(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext) error {
	cacheKey := cacheKey(params.Url, params)
	cacheValue, ok := cache.Get(cacheKey)
	if ok {
		counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
		counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		c.Set("Content-Type", cacheValue.ContentType)
		return c.Send(cacheValue.Body)
	}

	// First check if it's a video by doing a HEAD request
	responseContentType, err := validation.GetContentType(params.Url)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to check video")
	}

	if responseContentType == "" {
		return c.Status(fiber.StatusForbidden).SendString("no content type received")
	}

	parsedContentType, _, err := mime.ParseMediaType(responseContentType)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
	}

	if !validation.IsVideoMime(parsedContentType) {
		return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
	}

	// Extract frame from specified position
	frameImage, err := extractFrameFromPosition(params.Url, params.FramePosition)
	if err != nil {
		logger.Error("failed to extract frame", zap.Error(err), zap.String("position", params.FramePosition))
		return c.Status(fiber.StatusInternalServerError).SendString("failed to extract video preview")
	}

	if params.Width > 0 || params.Height > 0 {
		frameImage, err = resizeImage(frameImage, params.Width, params.Height, params.Interpolation)
		if err != nil {
			logger.Error("failed to resize image", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to resize image")
		}
	}

	if params.Scale > 0 {
		frameImage, err = rescaleImage(frameImage, params.Scale)
		if err != nil {
			logger.Error("failed to rescale image", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to rescale image")
		}
	}

	if params.Webp {
		buf := pool.GetBuffer()
		defer pool.PutBuffer(buf)

		options, err := encoder.NewLossyEncoderOptions(encoder.PresetDefault, float32(params.Quality))
		if err != nil {
			logger.Error("failed to create webp encoder options", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to create webp encoder options")
		}
		err = webp.Encode(buf, frameImage, options)
		if err != nil {
			logger.Error("failed to encode webp", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to encode webp")
		}

		cache.SetWithTTL(cacheKey, CacheValue{
			Body:        buf.Bytes(),
			ContentType: "image/webp",
		}, 1000, time.Duration(config.CacheTTL)*time.Second)

		c.Set("Content-Type", "image/webp")
		c.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", config.HTTPCacheTTL))

		logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

		counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		return c.Send(buf.Bytes())
	}

	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)

	err = jpeg.Encode(buf, frameImage, &jpeg.Options{Quality: params.Quality})
	if err != nil {
		logger.Error("failed to encode jpeg", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).SendString("failed to encode jpeg")
	}

	cache.SetWithTTL(cacheKey, CacheValue{
		Body:        buf.Bytes(),
		ContentType: "image/jpeg",
	}, 1000, time.Duration(config.CacheTTL)*time.Second)

	c.Set("Content-Type", "image/jpeg")
	c.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", config.HTTPCacheTTL))

	logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

	counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

	return c.Send(buf.Bytes())
}
