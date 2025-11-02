package routes

import (
	"context"
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
func RegisterVideoRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App, counters *metrics.Metrics, s3cache *S3Cache) {
	// New path-based route: /videos/preview/q:50/w:500/h:300/webp/{base64-encoded-url}
	app.Get("/videos/preview/*", handleVideoPreviewRequest(logger, cache, config, counters, s3cache))

	// Legacy query-based route for backward compatibility
	app.Get("/video/preview", handleVideoPreviewRequestLegacy(logger, cache, config, counters, s3cache))
}

//#region handleVideoPreviewRequest

// handleVideoPreviewRequest processes video preview requests with path parameters
func handleVideoPreviewRequest(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pathParams := c.Params("*")
		logger.Info("video preview request received", zap.String("pathParams", pathParams))

		ok, status, err, params := validation.ProcessImageContextFromPath(logger, pathParams, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		// Add debug logging for parsed parameters
		logger.Info("parsed video parameters",
			zap.Int("width", params.Width),
			zap.Int("height", params.Height),
			zap.Float64("scale", params.Scale),
			zap.String("framePosition", params.FramePosition),
			zap.String("url", params.Url))

		return processVideoPreview(c, logger, cache, config, counters, params, s3cache)
	}
}

//#endregion

//#region handleVideoPreviewRequestLegacy

// handleVideoPreviewRequestLegacy handles legacy query-based video preview requests
func handleVideoPreviewRequestLegacy(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("video preview request received", zap.String("url", c.Query("url")))

		ok, status, err, params := validation.ProcessImageContext(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processVideoPreview(c, logger, cache, config, counters, params, s3cache)
	}
}

//#endregion

//#region processVideoPreview

// processVideoPreview handles the common video preview processing logic
func processVideoPreview(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext, s3cache *S3Cache) error {
	// Add debug logging for parameters
	logger.Info("processing video preview",
		zap.Int("width", params.Width),
		zap.Int("height", params.Height),
		zap.Float64("scale", params.Scale),
		zap.String("framePosition", params.FramePosition),
		zap.String("url", params.Url))

	cacheKey := cacheKey(params.Url, params)
	cacheValue, ok := cache.Get(cacheKey)
	if ok {
		counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
		counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

		c.Set("Content-Type", cacheValue.ContentType)
		return c.Send(cacheValue.Body)
	}

	// Try S3 cache if enabled
	if s3cache != nil && s3cache.Enabled {
		if params.CustomObjectKey != "" {
			if s3val, err := s3cache.GetAtLocation(context.Background(), params.CustomObjectKey); err == nil && s3val != nil {
				counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
				counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

				c.Set("Content-Type", s3val.ContentType)
				return c.Send(s3val.Body)
			}
		}

		if s3val, err := s3cache.Get(context.Background(), cacheKey); err == nil && s3val != nil {
			counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()
			counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

			cache.SetWithTTL(cacheKey, *s3val, 1000, time.Duration(config.CacheTTL)*time.Second)

			c.Set("Content-Type", s3val.ContentType)
			return c.Send(s3val.Body)
		}
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

	// Add debug logging for frame extraction
	logger.Debug("frame extracted successfully",
		zap.Int("originalWidth", frameImage.Bounds().Dx()),
		zap.Int("originalHeight", frameImage.Bounds().Dy()))

	if params.Width > 0 || params.Height > 0 {
		logger.Debug("resizing frame", zap.Int("targetWidth", params.Width), zap.Int("targetHeight", params.Height))
		frameImage, err = resizeImage(frameImage, params.Width, params.Height, params.Interpolation)
		if err != nil {
			logger.Error("failed to resize image", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to resize image")
		}
		// Add debug logging after resize
		logger.Debug("frame resized successfully",
			zap.Int("newWidth", frameImage.Bounds().Dx()),
			zap.Int("newHeight", frameImage.Bounds().Dy()))
	}

	if params.Scale > 0 {
		logger.Debug("rescaling frame", zap.Float64("scale", params.Scale))
		frameImage, err = rescaleImage(frameImage, params.Scale)
		if err != nil {
			logger.Error("failed to rescale image", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to rescale image")
		}
		// Add debug logging after rescale
		logger.Debug("frame rescaled successfully",
			zap.Int("newWidth", frameImage.Bounds().Dx()),
			zap.Int("newHeight", frameImage.Bounds().Dy()))
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

	value := CacheValue{Body: buf.Bytes(), ContentType: "image/jpeg"}
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

	c.Set("Content-Type", "image/jpeg")
	c.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", config.HTTPCacheTTL))

	logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

	counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(params.Url)).Inc()

	return c.Send(buf.Bytes())
}

//#endregion
