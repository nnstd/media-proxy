package validation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"media-proxy/config"
	"media-proxy/pool"

	"github.com/gofiber/fiber/v2"
	"github.com/nfnt/resize"
	"go.uber.org/zap"
)

type ImageContext struct {
	Url string

	Quality int

	Width  int
	Height int

	Scale         float64
	Interpolation resize.InterpolationFunction

	Webp bool

	// Video-specific parameters
	FramePosition string // "first", "half", "last", or time in seconds

	Hostname string

	// Optional explicit S3 object key provided by request (requires signature)
	CustomObjectKey string
}

func (c *ImageContext) String() string {
	return fmt.Sprintf("quality=%d;width=%d;height=%d;scale=%f;interpolation=%d;webp=%t;framePosition=%s", c.Quality, c.Width, c.Height, c.Scale, c.Interpolation, c.Webp, c.FramePosition)
}

// PathParams holds the parsed parameters from the URL path
type PathParams struct {
	Quality       int
	Width         int
	Height        int
	Scale         float64
	Interpolation resize.InterpolationFunction
	Webp          bool
	FramePosition string
	Signature     string
	Token         string
	EncodedURL    string
	Location      string
}

// ParsePathParams extracts parameters from the URL path
// Expected format: /images/q:50/w:500/h:300/s:0.8/i:2/webp/fp:half/sig:abc123/{base64-url}
func ParsePathParams(pathParams string) (*PathParams, error) {
	params := &PathParams{
		Quality:       100, // default values
		Width:         0,
		Height:        0,
		Scale:         0,
		Interpolation: resize.Lanczos3,
		Webp:          false,
		FramePosition: "first", // default to first frame
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
		case "fp", "framePosition":
			params.FramePosition = value
		case "t", "token":
			params.Token = value
		case "loc", "location":
			params.Location = value
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

// DecodeBase64URL decodes a base64 URL-safe encoded string (keeps compatibility with URL-safe alphabet)
func DecodeBase64URL(encoded string) (string, error) {
	decoded, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
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

// compareHmacForMessage validates HMAC for an arbitrary message
func compareHmacForMessage(message, providedSignature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expectedMAC := mac.Sum(nil)
	providedMAC, err := hex.DecodeString(providedSignature)
	if err != nil {
		return false
	}
	return hmac.Equal(expectedMAC, providedMAC)
}

// sanitizeLocation ensures S3 object key is in an acceptable format
func sanitizeLocation(loc string) (string, error) {
	if len(loc) == 0 || len(loc) > 512 {
		return "", fmt.Errorf("invalid location length")
	}
	// Disallow parent traversal and backslashes
	if strings.Contains(loc, "..") || strings.Contains(loc, "\\") {
		return "", fmt.Errorf("invalid location characters")
	}
	// Only allow a conservative charset
	for _, r := range loc {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '/' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("invalid character in location")
	}
	// Trim leading slash to keep it relative to prefix
	loc = strings.TrimLeft(loc, "/")
	return loc, nil
}

// ProcessImageUploadFromPath processes image upload parameters from path
func ProcessImageUploadFromPath(logger *zap.Logger, pathParams string, config *config.Config) (bool, int, error, *ImageContext) {
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

	return true, fiber.StatusOK, nil, &ImageContext{
		Quality:       params.Quality,
		Width:         params.Width,
		Height:        params.Height,
		Scale:         params.Scale,
		Interpolation: params.Interpolation,
		Webp:          params.Webp,
		FramePosition: params.FramePosition,
	}
}

func ProcessImageUpload(logger *zap.Logger, c *fiber.Ctx, config *config.Config) (ok bool, status int, err error, params *ImageContext) {
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

	return true, fiber.StatusOK, nil, &ImageContext{
		Quality:       quality,
		Width:         width,
		Height:        height,
		Scale:         scale,
		Interpolation: resize.InterpolationFunction(interpolation),
		Webp:          webp,
	}
}

// ProcessImageContextFromPath processes image context from path parameters
func ProcessImageContextFromPath(logger *zap.Logger, pathParams string, config *config.Config) (bool, int, error, *ImageContext) {
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

	// If custom location is requested, require signature and validate URL|location
	customObjectKey := ""
	if params.Location != "" {
		if config.HmacKey == "" || params.Signature == "" {
			return false, fiber.StatusForbidden, fmt.Errorf("signature required for custom location"), nil
		}
		// Expect location to be base64 URL-safe encoded
		decodedLocation, derr := DecodeBase64URL(params.Location)
		if derr != nil {
			return false, fiber.StatusBadRequest, derr, nil
		}
		sanitized, serr := sanitizeLocation(decodedLocation)
		if serr != nil {
			return false, fiber.StatusBadRequest, serr, nil
		}
		signedMsg := urlParam + "|" + sanitized
		if !compareHmacForMessage(signedMsg, params.Signature, config.HmacKey) {
			return false, fiber.StatusForbidden, fmt.Errorf("invalid signature for location"), nil
		}
		customObjectKey = sanitized
	} else if params.Signature != "" { // normal signature over URL only
		if config.HmacKey == "" {
			return false, fiber.StatusForbidden, fmt.Errorf("hmac key is not set"), nil
		}
		if !compareHmac(urlParam, params.Signature, config.HmacKey) {
			return false, fiber.StatusForbidden, fmt.Errorf("invalid signature"), nil
		}
	}

	validOrigin, hostname := pool.ValidateUrl(logger, urlParam, config.AllowedOrigins)
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

	return true, fiber.StatusOK, nil, &ImageContext{
		Url:             urlParam,
		Quality:         params.Quality,
		Width:           params.Width,
		Height:          params.Height,
		Scale:           params.Scale,
		Interpolation:   params.Interpolation,
		Webp:            params.Webp,
		FramePosition:   params.FramePosition,
		Hostname:        hostname,
		CustomObjectKey: customObjectKey,
	}
}

func ProcessImageContext(logger *zap.Logger, c *fiber.Ctx, config *config.Config) (ok bool, status int, err error, params *ImageContext) {
	urlParam := c.Query("url")
	if urlParam == "" {
		return false, fiber.StatusBadRequest, fmt.Errorf("url is required"), nil
	}

	signature := c.Query("signature")
	location := c.Query("location")
	customObjectKey := ""
	if location != "" {
		if config.HmacKey == "" || signature == "" {
			return false, fiber.StatusForbidden, fmt.Errorf("signature required for custom location"), nil
		}
		// Expect location to be base64 URL-safe encoded
		decodedLocation, derr := DecodeBase64URL(location)
		if derr != nil {
			return false, fiber.StatusBadRequest, derr, nil
		}
		sanitized, serr := sanitizeLocation(decodedLocation)
		if serr != nil {
			return false, fiber.StatusBadRequest, serr, nil
		}
		signedMsg := urlParam + "|" + sanitized
		if !compareHmacForMessage(signedMsg, signature, config.HmacKey) {
			return false, fiber.StatusForbidden, fmt.Errorf("invalid signature for location"), nil
		}
		customObjectKey = sanitized
	} else if signature != "" {
		if config.HmacKey == "" {
			return false, fiber.StatusForbidden, fmt.Errorf("hmac key is not set"), nil
		}
		if !compareHmac(urlParam, signature, config.HmacKey) {
			return false, fiber.StatusForbidden, fmt.Errorf("invalid signature"), nil
		}
	}

	validOrigin, hostname := pool.ValidateUrl(logger, urlParam, config.AllowedOrigins)
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
	framePosition := c.Query("framePosition", "first")

	return true, fiber.StatusOK, nil, &ImageContext{
		Url:           urlParam,
		Quality:       quality,
		Width:         width,
		Height:        height,
		Scale:         scale,
		Interpolation: resize.InterpolationFunction(interpolation),
		Webp:          webp,
		FramePosition: framePosition,

		Hostname:        hostname,
		CustomObjectKey: customObjectKey,
	}
}

// ValidateFileSize checks if the file size is within acceptable limits
func ValidateFileSize(size int64, maxSizeMB int) error {
	if maxSizeMB <= 0 {
		return nil // No limit set
	}

	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024
	if size > maxSizeBytes {
		return fmt.Errorf("file size %d bytes exceeds maximum allowed size of %d MB", size, maxSizeMB)
	}

	return nil
}

// ValidateContentLength checks Content-Length header if present
func ValidateContentLength(contentLength string, maxSizeMB int) error {
	if contentLength == "" || maxSizeMB <= 0 {
		return nil
	}

	size, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid content length: %s", contentLength)
	}

	return ValidateFileSize(size, maxSizeMB)
}
