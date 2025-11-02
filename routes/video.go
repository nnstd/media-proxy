package routes

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"

	"image/jpeg"
	"media-proxy/client"
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

	// Proxy routes for raw video bytes (support Range)
	app.Get("/videos/*", handleVideoProxyRequest(logger, cache, config, counters, s3cache))
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

// handleVideoProxyRequest processes raw video proxy requests (path params)
func handleVideoProxyRequest(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pathParams := c.Params("*")
		logger.Info("video proxy request received", zap.String("pathParams", pathParams))

		ok, status, err, params := validation.ProcessImageContextFromPath(logger, pathParams, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		return processVideoProxy(c, logger, cache, config, counters, params, s3cache)
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

//#region parseRangeHeader

// parseRangeHeader parses a single Range header of the form "bytes=start-end".
// Returns start, end (end == -1 means to the end), hasRange, error
func parseRangeHeader(h string) (int64, int64, bool, error) {
	if h == "" {
		return 0, -1, false, nil
	}
	if !strings.HasPrefix(h, "bytes=") {
		return 0, -1, false, fmt.Errorf("unsupported range unit")
	}
	spec := strings.TrimPrefix(h, "bytes=")
	// only support single range
	if strings.Contains(spec, ",") {
		return 0, -1, false, fmt.Errorf("multiple ranges not supported")
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, -1, false, fmt.Errorf("invalid range")
	}
	if parts[0] == "" {
		// suffix range: -N (last N bytes) -> unsupported for S3 simple handling
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, -1, false, err
		}
		return -n, -1, true, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, -1, false, err
	}
	if parts[1] == "" {
		return start, -1, true, nil
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, -1, false, err
	}
	return start, end, true, nil
}

//#endregion

//#region processVideoProxy

// processVideoProxy streams raw video bytes from either S3 (explicit location) or HTTP/HTTPS origin.
// Supports Range requests and forwards relevant headers.
func processVideoProxy(c *fiber.Ctx, logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, counters *metrics.Metrics, params *validation.ImageContext, s3cache *S3Cache) error {
	logger.Info("processing video proxy", zap.String("url", params.Url), zap.String("location", params.CustomObjectKey))

	rangeHeader := c.Get("Range")

	// If explicit S3 location provided, fetch from S3 (signature already enforced in validation)
	if params.CustomObjectKey != "" && s3cache != nil && s3cache.Enabled && s3cache.Client != nil {
		objKey := objectKeyFromExplicitLocation(s3cache.Prefix, params.CustomObjectKey)
		opts := minio.GetObjectOptions{}
		start, end, hasRange, err := parseRangeHeader(rangeHeader)
		if err != nil {
			logger.Error("invalid range header", zap.Error(err))
			return c.Status(fiber.StatusRequestedRangeNotSatisfiable).SendString("invalid range")
		}
		if hasRange {
			// handle suffix-range -N (we translate into start = size - N when we know size)
			if start < 0 && end == -1 {
				// We'll handle after Stat when we know size; for now request whole object and we'll seek
				// But better: use SetRange with -n meaning last n bytes is not directly supported, so proceed without range and implement after Stat
			} else {
				if end == -1 {
					// until end
					opts.SetRange(start, -1)
				} else {
					opts.SetRange(start, end)
				}
			}
		}

		obj, err := s3cache.Client.GetObject(context.Background(), s3cache.Bucket, objKey, opts)
		if err != nil {
			logger.Error("failed to get object from s3", zap.Error(err), zap.String("object", objKey))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch object from s3")
		}
		// Ensure close when handler finishes
		defer func() {
			_ = obj.Close()
		}()

		info, err := obj.Stat()
		if err != nil {
			logger.Error("failed to stat s3 object", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to stat s3 object")
		}

		contentType := info.ContentType
		if contentType == "" {
			if ct, ok := info.Metadata["Content-Type"]; ok && len(ct) > 0 {
				contentType = ct[0]
			} else {
				contentType = "application/octet-stream"
			}
		}

		// If we had a suffix-range (-N), compute correct start/end now
		if rangeHeader != "" {
			start, end, hasRange, _ := parseRangeHeader(rangeHeader)
			if hasRange {
				total := info.Size
				if start < 0 {
					// suffix: last N bytes
					n := -start
					if n > total {
						start = 0
					} else {
						start = total - n
					}
					end = total - 1
				} else if end == -1 {
					end = info.Size - 1
				}
				if start < 0 || start >= info.Size || start > end {
					return c.Status(fiber.StatusRequestedRangeNotSatisfiable).SendString("range not satisfiable")
				}
				length := end - start + 1
				c.Set("Accept-Ranges", "bytes")
				c.Set("Content-Type", contentType)
				c.Set("Content-Length", strconv.FormatInt(length, 10))
				c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size))
				// Return Partial Content
				return c.Status(http.StatusPartialContent).SendStream(obj)
			}
		}

		// No range requested
		c.Set("Accept-Ranges", "bytes")
		c.Set("Content-Type", contentType)
		c.Set("Content-Length", strconv.FormatInt(info.Size, 10))
		return c.Status(http.StatusOK).SendStream(obj)
	}

	// Otherwise proxy via HTTP/HTTPS
	req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, params.Url, nil)
	if err != nil {
		logger.Error("failed to create request", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).SendString("failed to create request to origin")
	}
	// Forward Range header if present
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	resp, err := client.GetHTTPClient().Do(req)
	if err != nil {
		logger.Error("failed to fetch origin", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch origin")
	}
	// ensure body closed after streaming
	defer resp.Body.Close()

	// Forward major headers
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Set("Content-Type", ct)
	}
	if ar := resp.Header.Get("Accept-Ranges"); ar != "" {
		c.Set("Accept-Ranges", ar)
	} else {
		c.Set("Accept-Ranges", "bytes")
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		c.Set("Content-Range", cr)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		c.Set("Content-Length", cl)
	}

	// Pass through status code (200 or 206 expected)
	return c.Status(resp.StatusCode).SendStream(resp.Body)
}

//#endregion
