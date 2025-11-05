package routes

import (
	"context"
	"fmt"
	"io"
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
func RegisterVideoRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App, counters *metrics.Metrics, s3cache *S3Cache, uploadTracker *RedisUploadTracker) {
	// Multi-part upload routes (must be registered before wildcard routes)
	app.Post("/videos/multiparts", handleMultipartUploadInit(logger, config, uploadTracker))
	app.Post("/videos/multiparts/:uploadId/parts/:partIndex", handleMultipartUploadPart(logger, config, counters, s3cache, uploadTracker))
	app.Get("/videos/multiparts/:uploadId", handleMultipartUploadStatus(logger, config, uploadTracker))

	// Video upload route (single upload)
	app.Post("/videos", handleVideoUpload(logger, config, counters, s3cache))

	// New path-based route: /videos/preview/q:50/w:500/h:300/webp/{base64-encoded-url}
	app.Get("/videos/preview/*", handleVideoPreviewRequest(logger, cache, config, counters, s3cache))

	// Proxy routes for raw video bytes (support Range) - should be last as it's a catch-all
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
		zap.String("url", params.Url),
		zap.String("location", params.CustomObjectKey))

	// Ensure we have either URL or location
	if params.Url == "" && params.CustomObjectKey == "" {
		return c.Status(fiber.StatusBadRequest).SendString("either url or location is required")
	}

	// Use location as identifier if URL is not provided
	identifier := params.Url
	if identifier == "" && params.CustomObjectKey != "" {
		identifier = params.CustomObjectKey
	}

	cacheKey := cacheKey(identifier, params)
	cacheValue, ok := cache.Get(cacheKey)
	if ok {
		counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()
		counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()

		c.Set("Content-Type", cacheValue.ContentType)
		return c.Send(cacheValue.Body)
	}

	// Try S3 cache if enabled
	if s3cache != nil && s3cache.Enabled {
		if params.CustomObjectKey != "" {
			if s3val, err := s3cache.GetAtLocation(context.Background(), params.CustomObjectKey); err == nil && s3val != nil {
				counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()
				counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()

				c.Set("Content-Type", s3val.ContentType)
				return c.Send(s3val.Body)
			}
		}

		if s3val, err := s3cache.Get(context.Background(), cacheKey); err == nil && s3val != nil {
			counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()
			counters.ServedCached.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()

			cache.SetWithTTL(cacheKey, *s3val, 1000, time.Duration(config.CacheTTL)*time.Second)

			c.Set("Content-Type", s3val.ContentType)
			return c.Send(s3val.Body)
		}
	}

	var videoURL string
	var parsedContentType string

	// If explicit S3 location provided, use it directly (signature already enforced in validation)
	if params.CustomObjectKey != "" && s3cache != nil && s3cache.Enabled && s3cache.Client != nil {
		// Use S3 location as video source
		objKey := objectKeyFromExplicitLocation(s3cache.Prefix, params.CustomObjectKey)

		// Get object info to validate it's a video
		obj, err := s3cache.Client.StatObject(context.Background(), s3cache.Bucket, objKey, minio.StatObjectOptions{})
		if err != nil {
			logger.Error("failed to stat s3 object", zap.Error(err), zap.String("object", objKey))
			return c.Status(fiber.StatusNotFound).SendString("video not found in s3")
		}

		parsedContentType = obj.ContentType
		if parsedContentType == "" {
			if ct, ok := obj.Metadata["Content-Type"]; ok && len(ct) > 0 {
				parsedContentType = ct[0]
			} else {
				parsedContentType = "application/octet-stream"
			}
		}

		parsed, _, err := mime.ParseMediaType(parsedContentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}
		parsedContentType = parsed

		if !validation.IsVideoMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not a video", parsedContentType))
		}

		// Generate presigned URL for ffmpeg to access
		presignedURL, err := s3cache.Client.PresignedGetObject(context.Background(), s3cache.Bucket, objKey, time.Hour, nil)
		if err != nil {
			logger.Error("failed to generate presigned url", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to generate presigned url")
		}
		videoURL = presignedURL.String()
	} else {
		// Use HTTP/HTTPS origin - requires URL to be provided
		if params.Url == "" {
			return c.Status(fiber.StatusBadRequest).SendString("url is required when location is not provided")
		}

		responseContentType, err := validation.GetContentType(params.Url)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to check video")
		}

		if responseContentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsed, _, err := mime.ParseMediaType(responseContentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}
		parsedContentType = parsed

		if !validation.IsVideoMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		videoURL = params.Url
	}

	// Extract frame from specified position
	frameImage, err := extractFrameFromPosition(videoURL, params.FramePosition)
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

		logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("identifier", identifier))

		counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()

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

	logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("identifier", identifier))

	counters.SuccessfullyServed.WithLabelValues("video-preview", metrics.CleanHostname(params.Hostname), metrics.HashURL(identifier)).Inc()

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

//#region handleVideoUpload

// handleVideoUpload processes video upload requests
// Requires: deadline (unix timestamp), location (base64-encoded S3 key), signature (HMAC of deadline|location)
func handleVideoUpload(logger *zap.Logger, config *config.Config, counters *metrics.Metrics, s3cache *S3Cache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("video upload request received")

		// Validate upload parameters (deadline, location, signature)
		location, status, err := validation.ValidateVideoUpload(c, config)
		if err != nil {
			logger.Error("upload validation failed", zap.Error(err))
			return c.Status(status).SendString(err.Error())
		}

		// Check if S3 is enabled
		if s3cache == nil || !s3cache.Enabled || s3cache.Client == nil {
			logger.Error("S3 storage is not enabled or configured")
			return c.Status(fiber.StatusServiceUnavailable).SendString("video upload service unavailable")
		}

		// Get video file from multipart form
		fileHeader, err := c.FormFile("video")
		if err != nil {
			logger.Error("failed to get video file", zap.Error(err))
			return c.Status(fiber.StatusBadRequest).SendString("video file is required")
		}

		// Validate file size
		if config.MaxVideoSize > 0 {
			maxSizeBytes := int64(config.MaxVideoSize) * 1024 * 1024
			if fileHeader.Size > maxSizeBytes {
				logger.Error("video file too large", zap.Int64("size", fileHeader.Size), zap.Int("maxMB", config.MaxVideoSize))
				return c.Status(fiber.StatusRequestEntityTooLarge).SendString(fmt.Sprintf("video file exceeds maximum size of %d MB", config.MaxVideoSize))
			}
		}

		// Open and read video file
		file, err := fileHeader.Open()
		if err != nil {
			logger.Error("failed to open video file", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to open video file")
		}
		defer file.Close()

		// Validate content type
		contentType := fileHeader.Header.Get("Content-Type")
		if contentType == "" {
			logger.Error("no content type provided")
			return c.Status(fiber.StatusBadRequest).SendString("content type is required")
		}

		parsedContentType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			logger.Error("failed to parse content type", zap.Error(err))
			return c.Status(fiber.StatusBadRequest).SendString("invalid content type")
		}

		if !validation.IsVideoMime(parsedContentType) {
			logger.Error("invalid content type", zap.String("contentType", parsedContentType))
			return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("content type '%s' is not a video", parsedContentType))
		}

		// Read file contents
		videoData, err := io.ReadAll(file)
		if err != nil {
			logger.Error("failed to read video file", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to read video file")
		}

		// Upload to S3
		err = s3cache.PutAtLocation(context.Background(), location, videoData, parsedContentType)
		if err != nil {
			logger.Error("failed to upload video to S3", zap.Error(err), zap.String("location", location))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to upload video")
		}

		// Increment metrics
		counters.SuccessfullyServed.WithLabelValues("video-upload", "upload", "upload").Inc()

		logger.Info("video uploaded successfully",
			zap.String("location", location),
			zap.String("contentType", parsedContentType),
			zap.Int64("size", fileHeader.Size))

		// Return success with location
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"location": location,
			"size":     fileHeader.Size,
		})
	}
}

//#endregion

//#region handleMultipartUploadInit

// handleMultipartUploadInit initializes a multi-part upload session
// Required query params: token, deadline, location, size, contentType
// Optional: chunkSize
func handleMultipartUploadInit(logger *zap.Logger, config *config.Config, uploadTracker *RedisUploadTracker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("multipart upload init request received")

		// Check if uploading is enabled
		if !config.UploadingEnabled {
			return c.Status(fiber.StatusForbidden).SendString("video uploading is disabled")
		}

		// Check if Redis is configured
		if uploadTracker == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString("multi-part upload not configured")
		}

		// Validate token
		token := c.Query("token")
		if token == "" || token != config.Token {
			logger.Error("invalid or missing token")
			return c.Status(fiber.StatusForbidden).SendString("invalid token")
		}

		// Get deadline
		deadlineStr := c.Query("deadline")
		if deadlineStr == "" {
			return c.Status(fiber.StatusBadRequest).SendString("deadline parameter is required")
		}

		deadline, err := time.Parse(time.RFC3339, deadlineStr)
		if err != nil {
			// Try parsing as Unix timestamp
			var deadlineUnix int64
			if _, err := fmt.Sscanf(deadlineStr, "%d", &deadlineUnix); err == nil {
				deadline = time.Unix(deadlineUnix, 0)
			} else {
				return c.Status(fiber.StatusBadRequest).SendString("invalid deadline format")
			}
		}

		// Check if deadline has passed
		if time.Now().After(deadline) {
			return c.Status(fiber.StatusForbidden).SendString("upload deadline has expired")
		}

		// Get location
		location := c.Query("location")
		if location == "" {
			return c.Status(fiber.StatusBadRequest).SendString("location parameter is required")
		}

		// Get file size
		sizeStr := c.Query("size")
		if sizeStr == "" {
			return c.Status(fiber.StatusBadRequest).SendString("size parameter is required")
		}

		var totalSize int64
		if _, err := fmt.Sscanf(sizeStr, "%d", &totalSize); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("invalid size format")
		}

		if totalSize <= 0 {
			return c.Status(fiber.StatusBadRequest).SendString("size must be greater than 0")
		}

		// Validate file size
		if config.MaxVideoSize > 0 {
			maxSizeBytes := int64(config.MaxVideoSize) * 1024 * 1024
			if totalSize > maxSizeBytes {
				return c.Status(fiber.StatusRequestEntityTooLarge).SendString(fmt.Sprintf("video file exceeds maximum size of %d MB", config.MaxVideoSize))
			}
		}

		// Get content type
		contentType := c.Query("contentType")
		if contentType == "" {
			return c.Status(fiber.StatusBadRequest).SendString("contentType parameter is required")
		}

		parsedContentType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("invalid content type")
		}

		if !validation.IsVideoMime(parsedContentType) {
			return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("content type '%s' is not a video", parsedContentType))
		}

		// Get optional chunk size
		chunkSize := config.ChunkSize
		if chunkSize <= 0 {
			chunkSize = DefaultChunkSize
		}

		chunkSizeStr := c.Query("chunkSize")
		if chunkSizeStr != "" {
			var customChunkSize int64
			if _, err := fmt.Sscanf(chunkSizeStr, "%d", &customChunkSize); err == nil && customChunkSize > 0 {
				chunkSize = customChunkSize
			}
		}

		// Generate upload ID
		uploadID := fmt.Sprintf("%d", time.Now().UnixNano())

		// Initialize upload in Redis
		uploadInfo, err := uploadTracker.InitializeUpload(
			context.Background(),
			uploadID,
			location,
			totalSize,
			chunkSize,
			parsedContentType,
			deadline,
		)
		if err != nil {
			logger.Error("failed to initialize upload", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to initialize upload")
		}

		logger.Info("multipart upload initialized",
			zap.String("uploadId", uploadID),
			zap.String("location", location),
			zap.Int64("totalSize", totalSize),
			zap.Int("partsCount", uploadInfo.PartsCount))

		// Return upload information
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"uploadId":    uploadInfo.UploadID,
			"uploadToken": uploadInfo.UploadToken,
			"location":    uploadInfo.Location,
			"totalSize":   uploadInfo.TotalSize,
			"chunkSize":   uploadInfo.ChunkSize,
			"partsCount":  uploadInfo.PartsCount,
			"parts":       uploadInfo.Parts,
			"expiresAt":   uploadInfo.ExpiresAt,
		})
	}
}

//#endregion

//#region handleMultipartUploadPart

// handleMultipartUploadPart uploads a single part of a multi-part upload
// Required path params: uploadId, partIndex
// Required query params: uploadToken (generated during init, not APP_TOKEN)
func handleMultipartUploadPart(logger *zap.Logger, config *config.Config, counters *metrics.Metrics, s3cache *S3Cache, uploadTracker *RedisUploadTracker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("multipart upload part request received")

		// Check if uploading is enabled
		if !config.UploadingEnabled {
			return c.Status(fiber.StatusForbidden).SendString("video uploading is disabled")
		}

		// Check if Redis is configured
		if uploadTracker == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString("multi-part upload not configured")
		}

		// Check if S3 is enabled
		if s3cache == nil || !s3cache.Enabled || s3cache.Client == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString("video upload service unavailable")
		}

		// Get upload ID from path parameter
		uploadID := c.Params("uploadId")
		if uploadID == "" {
			return c.Status(fiber.StatusBadRequest).SendString("uploadId parameter is required")
		}

		// Get upload info first to validate token
		uploadInfo, err := uploadTracker.GetUploadInfo(context.Background(), uploadID)
		if err != nil {
			logger.Error("failed to get upload info", zap.Error(err))
			return c.Status(fiber.StatusNotFound).SendString("upload not found or expired")
		}

		// Validate upload token (not APP_TOKEN)
		uploadToken := c.Query("uploadToken")
		if uploadToken == "" || uploadToken != uploadInfo.UploadToken {
			logger.Error("invalid or missing upload token")
			return c.Status(fiber.StatusForbidden).SendString("invalid upload token")
		}

		// Get part index from path parameter
		partIndexStr := c.Params("partIndex")
		if partIndexStr == "" {
			return c.Status(fiber.StatusBadRequest).SendString("partIndex parameter is required")
		}

		var partIndex int
		if _, err := fmt.Sscanf(partIndexStr, "%d", &partIndex); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("invalid partIndex format")
		}

		// Check if deadline has passed
		if time.Now().After(uploadInfo.ExpiresAt) {
			return c.Status(fiber.StatusForbidden).SendString("upload deadline has expired")
		}

		// Validate part index
		if partIndex < 0 || partIndex >= uploadInfo.PartsCount {
			return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("invalid partIndex: must be between 0 and %d", uploadInfo.PartsCount-1))
		}

		// Get the part info
		part := uploadInfo.Parts[partIndex]

		// Get video part from multipart form
		fileHeader, err := c.FormFile("video")
		if err != nil {
			logger.Error("failed to get video file", zap.Error(err))
			return c.Status(fiber.StatusBadRequest).SendString("video file is required")
		}

		// Validate part size
		if fileHeader.Size != part.Size {
			return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("part size mismatch: expected %d bytes, got %d bytes", part.Size, fileHeader.Size))
		}

		// Open and read video part
		file, err := fileHeader.Open()
		if err != nil {
			logger.Error("failed to open video file", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to open video file")
		}
		defer file.Close()

		// Read file contents
		videoData, err := io.ReadAll(file)
		if err != nil {
			logger.Error("failed to read video file", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to read video file")
		}

		// Upload part to S3 with part suffix
		partLocation := fmt.Sprintf("%s.part%d", uploadInfo.Location, partIndex)
		err = s3cache.PutAtLocationExpiring(context.Background(), partLocation, videoData, uploadInfo.ContentType, uploadInfo.ExpiresAt)
		if err != nil {
			logger.Error("failed to upload video part to S3", zap.Error(err), zap.String("location", partLocation))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to upload video part")
		}

		// Mark part as uploaded
		err = uploadTracker.MarkPartUploaded(context.Background(), uploadID, partIndex)
		if err != nil {
			logger.Error("failed to mark part as uploaded", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).SendString("failed to update upload status")
		}

		// Check if upload is complete
		isComplete, err := uploadTracker.IsUploadComplete(context.Background(), uploadID)
		if err != nil {
			logger.Error("failed to check upload completion", zap.Error(err))
		}

		// Increment metrics
		counters.SuccessfullyServed.WithLabelValues("video-upload-part", "upload", "upload").Inc()

		logger.Info("video part uploaded successfully",
			zap.String("uploadId", uploadID),
			zap.Int("partIndex", partIndex),
			zap.String("location", partLocation),
			zap.Int64("size", fileHeader.Size),
			zap.Bool("complete", isComplete))

		response := fiber.Map{
			"uploadId":  uploadID,
			"partIndex": partIndex,
			"size":      fileHeader.Size,
			"complete":  isComplete,
		}

		return c.Status(fiber.StatusOK).JSON(response)
	}
}

//#endregion

//#region handleMultipartUploadStatus

// handleMultipartUploadStatus returns the status of a multi-part upload
// Requires token authentication
func handleMultipartUploadStatus(logger *zap.Logger, config *config.Config, uploadTracker *RedisUploadTracker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logger.Info("multipart upload status request received")

		// Check if Redis is configured
		if uploadTracker == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString("multi-part upload not configured")
		}

		// Validate token
		token := c.Query("token")
		if token == "" || token != config.Token {
			logger.Error("invalid or missing token")
			return c.Status(fiber.StatusForbidden).SendString("invalid token")
		}

		// Get upload ID from path parameter
		uploadID := c.Params("uploadId")
		if uploadID == "" {
			return c.Status(fiber.StatusBadRequest).SendString("uploadId is required")
		}

		// Get upload info
		uploadInfo, err := uploadTracker.GetUploadInfo(context.Background(), uploadID)
		if err != nil {
			logger.Error("failed to get upload info", zap.Error(err))
			return c.Status(fiber.StatusNotFound).SendString("upload not found or expired")
		}

		// Check if complete
		isComplete := len(uploadInfo.UploadedParts) == uploadInfo.PartsCount

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"id":            uploadInfo.UploadID,
			"location":      uploadInfo.Location,
			"totalSize":     uploadInfo.TotalSize,
			"partsCount":    uploadInfo.PartsCount,
			"uploadedParts": uploadInfo.UploadedParts,
			"uploadedCount": len(uploadInfo.UploadedParts),
			"complete":      isComplete,
			"contentType":   uploadInfo.ContentType,
			"createdAt":     uploadInfo.CreatedAt,
			"expiresAt":     uploadInfo.ExpiresAt,
		})
	}
}

//#endregion
