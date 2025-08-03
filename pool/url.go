package pool

import (
	"net/url"
	"strings"
	"sync"

	"github.com/IGLOU-EU/go-wildcard/v2"
	"go.uber.org/zap"
)

// URL cache for parsed URLs to avoid repeated parsing
var (
	urlCache     = make(map[string]*url.URL)
	urlCacheMux  sync.RWMutex
	urlCacheSize = 1000 // Limit cache size
)

func ValidateUrl(logger *zap.Logger, urlStr string, origins []string) (valid bool, hostname string) {
	// Check cache first
	urlCacheMux.RLock()
	if parsedUrl, exists := urlCache[urlStr]; exists {
		urlCacheMux.RUnlock()
		return ValidateHostname(parsedUrl, origins, logger)
	}
	urlCacheMux.RUnlock()

	// Parse URL if not in cache
	parsedUrl, err := url.Parse(urlStr)
	if err != nil {
		return false, ""
	}

	// Cache the parsed URL
	urlCacheMux.Lock()
	if len(urlCache) >= urlCacheSize {
		// Simple eviction: clear cache when it gets too large
		urlCache = make(map[string]*url.URL)
	}
	urlCache[urlStr] = parsedUrl
	urlCacheMux.Unlock()

	return ValidateHostname(parsedUrl, origins, logger)
}

func ValidateHostname(parsedUrl *url.URL, origins []string, logger *zap.Logger) (valid bool, hostname string) {
	if len(origins) == 0 {
		return true, ""
	}

	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return false, ""
	}

	hostname = parsedUrl.Hostname()

	// Early return for exact matches
	for _, origin := range origins {
		if origin == hostname {
			logger.Debug("origin matched", zap.String("origin", origin), zap.String("hostname", hostname))
			return true, hostname
		}
	}

	// Check wildcard patterns only if no exact match found
	for _, origin := range origins {
		if strings.Contains(origin, "*") && wildcard.Match(origin, hostname) {
			logger.Debug("origin matched", zap.String("origin", origin), zap.String("hostname", hostname))
			return true, hostname
		}
	}

	return false, ""
}
