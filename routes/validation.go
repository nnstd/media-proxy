package routes

import (
	"fmt"
	"media-proxy/config"

	"github.com/gofiber/fiber/v2"
	"github.com/nfnt/resize"
	"go.uber.org/zap"
)

type imageContext struct {
	Url string

	Quality int

	Width  int
	Height int

	Scale         float64
	Interpolation resize.InterpolationFunction

	Webp bool

	Hostname string
}

func (c *imageContext) String() string {
	return fmt.Sprintf("quality=%d;width=%d;height=%d;scale=%f;interpolation=%d;webp=%t", c.Quality, c.Width, c.Height, c.Scale, c.Interpolation, c.Webp)
}

func processImageContext(logger *zap.Logger, c *fiber.Ctx, config *config.Config) (ok bool, status int, err error, params *imageContext) {
	urlParam := c.Query("url")
	if urlParam == "" {
		return false, fiber.StatusBadRequest, fmt.Errorf("url is required"), nil
	}

	validOrigin, hostname := validateUrl(logger, urlParam, config.AllowedOrigins)
	if !validOrigin {
		return false, fiber.StatusForbidden, fmt.Errorf("url is not allowed"), nil
	}

	quality := c.QueryInt("quality", 100)
	if quality < 1 || quality > 100 {
		return false, fiber.StatusBadRequest, fmt.Errorf("quality must be between 1 and 100"), nil
	}

	interpolation := c.QueryInt("interpolation", int(resize.Lanczos3))
	if interpolation < 0 || interpolation > 5 {
		return false, fiber.StatusBadRequest, fmt.Errorf("interpolation must be between 0 and 5"), nil
	}

	width := c.QueryInt("width", 0)
	height := c.QueryInt("height", 0)
	if width > 0 && height > 0 && (width < 1 || height < 1) {
		return false, fiber.StatusBadRequest, fmt.Errorf("width and height must be greater than 0"), nil
	}

	scale := c.QueryFloat("scale", 0)
	if scale < 0 || scale > 1 {
		return false, fiber.StatusBadRequest, fmt.Errorf("scale must be between 0 and 1"), nil
	}

	webp := c.QueryBool("webp", config.Webp)

	return true, fiber.StatusOK, nil, &imageContext{
		Url:           urlParam,
		Quality:       quality,
		Width:         width,
		Height:        height,
		Scale:         scale,
		Interpolation: resize.InterpolationFunction(interpolation),
		Webp:          webp,

		Hostname: hostname,
	}
}
