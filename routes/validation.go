package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

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

// PathParams holds the parsed parameters from the URL path
type PathParams struct {
	Quality       int
	Width         int
	Height        int
	Scale         float64
	Interpolation resize.InterpolationFunction
	Webp          bool
	Signature     string
	Token         string
	EncodedURL    string
}

// ParsePathParams extracts parameters from the URL path
// Expected format: /images/q:50/w:500/h:300/s:0.8/i:2/webp/sig:abc123/{base64-url}
func ParsePathParams(pathParams string) (*PathParams, error) {
	params := &PathParams{
		Quality:       100, // default values
		Width:         0,
		Height:        0,
		Scale:         0,
		Interpolation: resize.Lanczos3,
		Webp:          false,
	}

	parts := strings.Split(strings.Trim(pathParams, "/"), "/")
	if len(parts) == 0 {
		return nil, fmt.Errorf("no path parameters found")
	}

	// The last part should be the encoded URL
	if len(parts) > 0 {
		params.EncodedURL = parts[len(parts)-1]
		parts = parts[:len(parts)-1] // Remove the URL from processing
	}

	for _, part := range parts {
		if part == "webp" {
			params.Webp = true
			continue
		}

		if !strings.Contains(part, ":") {
			continue // Skip malformed parameters
		}

		keyValue := strings.SplitN(part, ":", 2)
		if len(keyValue) != 2 {
			continue // Skip malformed parameters
		}

		key, value := keyValue[0], keyValue[1]

		switch key {
		case "q", "quality":
			if q, err := strconv.Atoi(value); err == nil && q >= 1 && q <= 100 {
				params.Quality = q
			}
		case "w", "width":
			if w, err := strconv.Atoi(value); err == nil && w > 0 {
				params.Width = w
			}
		case "h", "height":
			if h, err := strconv.Atoi(value); err == nil && h > 0 {
				params.Height = h
			}
		case "s", "scale":
			if s, err := strconv.ParseFloat(value, 64); err == nil && s > 0 && s <= 1 {
				params.Scale = s
			}
		case "i", "interpolation":
			if i, err := strconv.Atoi(value); err == nil && i >= 0 && i <= 5 {
				params.Interpolation = resize.InterpolationFunction(i)
			}
		case "sig", "signature":
			params.Signature = value
		case "t", "token":
			params.Token = value
		}
	}

	return params, nil
}

// DecodeURL decodes a base64-encoded URL
func DecodeURL(encodedURL string) (string, error) {
	decoded, err := base64.URLEncoding.DecodeString(encodedURL)
	if err != nil {
		return "", fmt.Errorf("failed to decode URL: %w", err)
	}
	return string(decoded), nil
}

func compareHmac(url, providedSignature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(url))
	expectedMAC := mac.Sum(nil)

	// Decode provided signature (hex to bytes)
	providedMAC, err := hex.DecodeString(providedSignature)
	if err != nil {
		return false
	}

	// Use constant-time comparison
	return hmac.Equal(expectedMAC, providedMAC)
}

// ProcessImageUploadFromPath processes image upload parameters from path
func ProcessImageUploadFromPath(logger *zap.Logger, pathParams string, config *config.Config) (bool, int, error, *imageContext) {
	params, err := ParsePathParams(pathParams)
	if err != nil {
		return false, fiber.StatusBadRequest, fmt.Errorf("invalid path parameters: %w", err), nil
	}

	if params.Token != config.Token {
		return false, fiber.StatusForbidden, fmt.Errorf("invalid token"), nil
	}

	if params.Quality < 1 || params.Quality > 100 {
		return false, fiber.StatusBadRequest, fmt.Errorf("quality must be between 1 and 100"), nil
	}

	if params.Width > 0 && params.Height > 0 && (params.Width < 1 || params.Height < 1) {
		return false, fiber.StatusBadRequest, fmt.Errorf("width and height must be greater than 0"), nil
	}

	if params.Scale < 0 || params.Scale > 1 {
		return false, fiber.StatusBadRequest, fmt.Errorf("scale must be between 0 and 1"), nil
	}

	// Apply default webp setting if not specified
	if !params.Webp && config.Webp {
		params.Webp = config.Webp
	}

	return true, fiber.StatusOK, nil, &imageContext{
		Quality:       params.Quality,
		Width:         params.Width,
		Height:        params.Height,
		Scale:         params.Scale,
		Interpolation: params.Interpolation,
		Webp:          params.Webp,
	}
}

func processImageUpload(logger *zap.Logger, c *fiber.Ctx, config *config.Config) (ok bool, status int, err error, params *imageContext) {
	token := c.Query("token")
	if token != config.Token {
		return false, fiber.StatusForbidden, fmt.Errorf("invalid token"), nil
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
		Quality:       quality,
		Width:         width,
		Height:        height,
		Scale:         scale,
		Interpolation: resize.InterpolationFunction(interpolation),
		Webp:          webp,
	}
}

// ProcessImageContextFromPath processes image context from path parameters
func ProcessImageContextFromPath(logger *zap.Logger, pathParams string, config *config.Config) (bool, int, error, *imageContext) {
	params, err := ParsePathParams(pathParams)
	if err != nil {
		return false, fiber.StatusBadRequest, fmt.Errorf("invalid path parameters: %w", err), nil
	}

	if params.EncodedURL == "" {
		return false, fiber.StatusBadRequest, fmt.Errorf("encoded URL is required"), nil
	}

	urlParam, err := DecodeURL(params.EncodedURL)
	if err != nil {
		return false, fiber.StatusBadRequest, fmt.Errorf("failed to decode URL: %w", err), nil
	}

	if params.Signature != "" {
		if config.HmacKey == "" {
			return false, fiber.StatusForbidden, fmt.Errorf("hmac key is not set"), nil
		}

		if !compareHmac(urlParam, params.Signature, config.HmacKey) {
			return false, fiber.StatusForbidden, fmt.Errorf("invalid signature"), nil
		}
	}

	validOrigin, hostname := validateUrl(logger, urlParam, config.AllowedOrigins)
	if !validOrigin {
		return false, fiber.StatusForbidden, fmt.Errorf("url is not allowed"), nil
	}

	if params.Quality < 1 || params.Quality > 100 {
		return false, fiber.StatusBadRequest, fmt.Errorf("quality must be between 1 and 100"), nil
	}

	if params.Width > 0 && params.Height > 0 && (params.Width < 1 || params.Height < 1) {
		return false, fiber.StatusBadRequest, fmt.Errorf("width and height must be greater than 0"), nil
	}

	if params.Scale < 0 || params.Scale > 1 {
		return false, fiber.StatusBadRequest, fmt.Errorf("scale must be between 0 and 1"), nil
	}

	// Apply default webp setting if not specified
	if !params.Webp && config.Webp {
		params.Webp = config.Webp
	}

	return true, fiber.StatusOK, nil, &imageContext{
		Url:           urlParam,
		Quality:       params.Quality,
		Width:         params.Width,
		Height:        params.Height,
		Scale:         params.Scale,
		Interpolation: params.Interpolation,
		Webp:          params.Webp,
		Hostname:      hostname,
	}
}

func processImageContext(logger *zap.Logger, c *fiber.Ctx, config *config.Config) (ok bool, status int, err error, params *imageContext) {
	urlParam := c.Query("url")
	if urlParam == "" {
		return false, fiber.StatusBadRequest, fmt.Errorf("url is required"), nil
	}

	signature := c.Query("signature")
	if signature != "" {
		if config.HmacKey == "" {
			return false, fiber.StatusForbidden, fmt.Errorf("hmac key is not set"), nil
		}

		if !compareHmac(urlParam, signature, config.HmacKey) {
			return false, fiber.StatusForbidden, fmt.Errorf("invalid signature"), nil
		}
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
