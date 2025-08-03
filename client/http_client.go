package client

import (
	"net/http"
	"time"
)

var httpClient *http.Client

func init() {
	// Create a custom HTTP client with optimized settings
	transport := &http.Transport{
		MaxIdleConns:        100,              // Maximum number of idle connections
		MaxIdleConnsPerHost: 10,               // Maximum idle connections per host
		IdleConnTimeout:     90 * time.Second, // How long to keep idle connections
		TLSHandshakeTimeout: 10 * time.Second, // TLS handshake timeout
		DisableCompression:  false,            // Enable compression
		ForceAttemptHTTP2:   true,             // Enable HTTP/2
	}

	httpClient = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second, // Overall request timeout
	}
}

// GetHTTPClient returns the optimized HTTP client
func GetHTTPClient() *http.Client {
	return httpClient
}
