package metrics

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	SuccessfullyServed *prometheus.CounterVec
	ServedCached       *prometheus.CounterVec
}

func InitializeMetrics(registry prometheus.Registerer, constLabels prometheus.Labels) *Metrics {
	metrics := &Metrics{
		SuccessfullyServed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "successfully_served",
			Help:        "Number of successfully served requests",
			ConstLabels: constLabels,
		}, []string{"type", "hostname", "url_hash"}), // Use URL hash instead of full URL
		ServedCached: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "served_cached",
			Help:        "Number of served responses from cache",
			ConstLabels: constLabels,
		}, []string{"type", "hostname", "url_hash"}), // Use URL hash instead of full URL
	}

	// Register the custom metrics with the Prometheus registry
	registry.MustRegister(metrics.SuccessfullyServed)
	registry.MustRegister(metrics.ServedCached)

	return metrics
}

// HashURL creates a short hash of the URL to reduce metric cardinality
func HashURL(url string) string {
	// Truncate URL if too long to prevent extremely long URLs from affecting hash performance
	if len(url) > 100 {
		url = url[:100]
	}

	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes for shorter hash
}

// CleanHostname removes port numbers and normalizes hostname for metrics
func CleanHostname(hostname string) string {
	if hostname == "" {
		return "unknown"
	}

	// Remove port if present
	if idx := strings.Index(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	// Limit hostname length
	if len(hostname) > 50 {
		hostname = hostname[:50]
	}

	return hostname
}
