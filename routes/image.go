package routes

import (
	"fmt"
	"io"
	goMime "mime"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"media-proxy/config"
	"media-proxy/mime"

	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
)

func RegisterImageRoutes(logger *zap.Logger, config *config.Config, app *fiber.App) {
	app.Get("/image", func(c *fiber.Ctx) error {
		logger.Info("image request received", zap.String("url", c.Query("url")))

		ok, status, err, params := processImageContext(logger, c, config)
		if !ok {
			return c.Status(status).SendString(err.Error())
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

		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				logger.Error(err.Error())
			}
		}(response.Body)

		if params.Quality != 100 || params.Webp {
			img, err := readImage(response.Body, parsedContentType)
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

			webp.Encode(c, img, &encoder.Options{
				Lossless: true,
				Quality:  float32(params.Quality),
			})
		} else {
			c.Set("Content-Type", parsedContentType)

			_, err = io.Copy(c, response.Body)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("failed to copy image")
			}
		}

		logger.Info("image served successfully", zap.String("content-type", parsedContentType), zap.String("origin", params.Hostname), zap.String("url", params.Url))

		return c.SendStatus(fiber.StatusOK)
	})
}
