package routes

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"image/jpeg"
	"media-proxy/config"
	"media-proxy/mime"
	goMime "mime"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
)

func RegisterVideoRoutes(logger *zap.Logger, cache *ristretto.Cache[string, CacheValue], config *config.Config, app *fiber.App) {
	app.Get("/video/preview", func(c *fiber.Ctx) error {
		logger.Info("video preview request received", zap.String("url", c.Query("url")))

		ok, status, err, params := processImageContext(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
		}

		cacheKey := cacheKey(params.Url, params)
		cacheValue, ok := cache.Get(cacheKey)
		if ok {
			c.Set("Content-Type", cacheValue.ContentType)
			return c.Send(cacheValue.Body)
		}

		// First check if it's a video by doing a HEAD request
		headResp, err := http.Get(params.Url)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to check video")
		}
		headResp.Body.Close()

		responseContentType := headResp.Header.Get("Content-Type")
		if responseContentType == "" {
			return c.Status(fiber.StatusForbidden).SendString("no content type received")
		}

		parsedContentType, _, err := goMime.ParseMediaType(responseContentType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to parse content type")
		}

		if !mime.IsVideoMime(parsedContentType) {
			return c.Status(fiber.StatusForbidden).SendString(fmt.Sprintf("content type '%s' is not allowed", parsedContentType))
		}

		// Extract first frame using streaming approach
		frameImage, err := extractFirstFrame(params.Url)
		if err != nil {
			logger.Error("failed to extract first frame", zap.Error(err))
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
			buf := bytes.NewBuffer(nil)
			err = webp.Encode(buf, frameImage, &encoder.Options{
				Lossless: true,
				Quality:  float32(params.Quality),
			})
			if err != nil {
				logger.Error("failed to encode webp", zap.Error(err))
				return c.Status(fiber.StatusInternalServerError).SendString("failed to encode webp")
			}

			cache.SetWithTTL(cacheKey, CacheValue{
				Body:        buf.Bytes(),
				ContentType: "image/webp",
			}, 1000, time.Duration(config.CacheTTL)*time.Second)

			c.Set("Content-Type", "image/webp")

			logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

			return c.Send(buf.Bytes())
		} else {
			buf := bytes.NewBuffer(nil)
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

			logger.Info("video preview served successfully", zap.String("original-content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

			return c.Send(buf.Bytes())
		}
	})
}
