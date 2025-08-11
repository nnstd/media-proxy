package validation

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "net/http"
    "testing"

    "github.com/gofiber/fiber/v2"
    "go.uber.org/zap"

    "media-proxy/config"
)

func hexHMAC(message, secret string) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(message))
    return hex.EncodeToString(mac.Sum(nil))
}

func TestCompareHmac_ValidAndInvalid(t *testing.T) {
    secret := "test-secret"
    url := "https://example.com/path/to/file.jpg"
    validSig := hexHMAC(url, secret)

    if !compareHmac(url, validSig, secret) {
        t.Fatalf("expected compareHmac to return true for valid signature")
    }

    if compareHmac(url, "deadbeef", secret) {
        t.Fatalf("expected compareHmac to return false for invalid signature")
    }

    // message variant
    msg := url + "|" + "uploads/2025/08/file.jpg"
    validMsgSig := hexHMAC(msg, secret)
    if !compareHmacForMessage(msg, validMsgSig, secret) {
        t.Fatalf("expected compareHmacForMessage to return true for valid signature")
    }
    if compareHmacForMessage(msg, "deadbeef", secret) {
        t.Fatalf("expected compareHmacForMessage to return false for invalid signature")
    }
}

func TestProcessImageContextFromPath_URLOnlySignature_Valid(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    url := "https://example.com/media/cat.jpg"
    sig := hexHMAC(url, secret)
    encoded := base64.URLEncoding.EncodeToString([]byte(url))
    pathParams := "sig:" + sig + "/" + encoded

    ok, status, err, ctx := ProcessImageContextFromPath(logger, pathParams, cfg)
    if !ok || status != http.StatusOK || err != nil {
        t.Fatalf("expected OK, got ok=%v status=%d err=%v", ok, status, err)
    }
    if ctx == nil || ctx.Url != url || ctx.CustomObjectKey != "" {
        t.Fatalf("unexpected ctx: %+v", ctx)
    }
}

func TestProcessImageContextFromPath_URLAndLocationSignature_Valid(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    url := "https://example.com/media/cat.jpg"
    // Note: location must be base64 URL-safe encoded in path-style
    location := "uploads-2025-08-cat.jpg" // must pass sanitizeLocation
    msg := url + "|" + location
    sig := hexHMAC(msg, secret)

    encoded := base64.URLEncoding.EncodeToString([]byte(url))
    encodedLocation := base64.URLEncoding.EncodeToString([]byte(location))
    pathParams := "loc:" + encodedLocation + "/sig:" + sig + "/" + encoded

    ok, status, err, ctx := ProcessImageContextFromPath(logger, pathParams, cfg)
    if !ok || status != http.StatusOK || err != nil {
        t.Fatalf("expected OK, got ok=%v status=%d err=%v", ok, status, err)
    }
    if ctx == nil || ctx.Url != url || ctx.CustomObjectKey != location {
        t.Fatalf("unexpected ctx: %+v", ctx)
    }
}

func TestProcessImageContextFromPath_LocationMissingSignature(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    url := "https://example.com/media/cat.jpg"
    encoded := base64.URLEncoding.EncodeToString([]byte(url))
    encLoc := base64.URLEncoding.EncodeToString([]byte("uploads/2025/08/cat.jpg"))
    pathParams := "loc:" + encLoc + "/" + encoded

    ok, status, err, _ := ProcessImageContextFromPath(logger, pathParams, cfg)
    if ok || status != http.StatusForbidden || err == nil {
        t.Fatalf("expected forbidden due to missing signature, got ok=%v status=%d err=%v", ok, status, err)
    }
}

func TestProcessImageContextFromPath_InvalidSignature(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    url := "https://example.com/media/cat.jpg"
    encoded := base64.URLEncoding.EncodeToString([]byte(url))
    // wrong signature for URL
    pathParams := "sig:deadbeef/" + encoded

    ok, status, err, _ := ProcessImageContextFromPath(logger, pathParams, cfg)
    if ok || status != http.StatusForbidden || err == nil {
        t.Fatalf("expected forbidden due to invalid signature, got ok=%v status=%d err=%v", ok, status, err)
    }
}

func TestProcessImageContext_QueryFlow_URLOnlySignature_Valid(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    app := fiber.New()
    app.Get("/images", func(c *fiber.Ctx) error {
        ok, status, err, _ := ProcessImageContext(logger, c, cfg)
        if !ok {
            return c.Status(status).SendString(err.Error())
        }
        return c.SendStatus(status)
    })

    url := "https://example.com/media/cat.jpg"
    sig := hexHMAC(url, secret)
    req, _ := http.NewRequest(http.MethodGet, "/images?url="+url+"&signature="+sig, nil)
    resp, err := app.Test(req)
    if err != nil {
        t.Fatalf("app.Test error: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
}

func TestProcessImageContext_QueryFlow_URLAndLocationSignature_Valid(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    app := fiber.New()
    app.Get("/images", func(c *fiber.Ctx) error {
        ok, status, err, _ := ProcessImageContext(logger, c, cfg)
        if !ok {
            return c.Status(status).SendString(err.Error())
        }
        return c.SendStatus(status)
    })

    url := "https://example.com/media/cat.jpg"
    location := "uploads/2025/08/cat.jpg"
    msg := url + "|" + location
    sig := hexHMAC(msg, secret)
    encodedLocation := base64.URLEncoding.EncodeToString([]byte(location))
    req, _ := http.NewRequest(http.MethodGet, "/images?url="+url+"&location="+encodedLocation+"&signature="+sig, nil)
    resp, err := app.Test(req)
    if err != nil {
        t.Fatalf("app.Test error: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
}

func TestProcessImageContext_QueryFlow_InvalidSignature(t *testing.T) {
    logger := zap.NewNop()
    secret := "test-secret"
    cfg := &config.Config{
        HmacKey:        secret,
        AllowedOrigins: []string{"example.com"},
    }

    app := fiber.New()
    app.Get("/images", func(c *fiber.Ctx) error {
        ok, status, err, _ := ProcessImageContext(logger, c, cfg)
        if !ok {
            return c.Status(status).SendString(err.Error())
        }
        return c.SendStatus(status)
    })

    url := "https://example.com/media/cat.jpg"
    req, _ := http.NewRequest(http.MethodGet, "/images?url="+url+"&signature=deadbeef", nil)
    resp, err := app.Test(req)
    if err != nil {
        t.Fatalf("app.Test error: %v", err)
    }
    if resp.StatusCode != http.StatusForbidden {
        t.Fatalf("expected 403, got %d", resp.StatusCode)
    }
}


