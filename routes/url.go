package routes

import (
	"net/url"
	"strings"

	"github.com/IGLOU-EU/go-wildcard/v2"
	"go.uber.org/zap"
)

func validateUrl(logger *zap.Logger, urlStr string, origins []string) (valid bool, hostname string) {
	parsedUrl, err := url.Parse(urlStr)
	if err != nil {
		return false, ""
	}

	if len(origins) == 0 {
		return true, ""
	}

	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return false, ""
	}

	for _, origin := range origins {
		hostname = parsedUrl.Hostname()

		logger.Debug("matching origin", zap.String("origin", origin), zap.String("hostname", hostname))

		if origin == hostname {
			logger.Debug("origin matched", zap.String("origin", origin), zap.String("hostname", hostname))
			return true, hostname
		} else if strings.Contains(origin, "*") && wildcard.Match(origin, hostname) {
			logger.Debug("origin matched", zap.String("origin", origin), zap.String("hostname", hostname))
			return true, hostname
		}
	}

	return false, ""
}
