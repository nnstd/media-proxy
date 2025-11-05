package validation

import (
	"testing"
)

func TestParsePathParams_WithLocation(t *testing.T) {
	// Test case: URL with location parameter (no encoded URL at the end)
	pathParams := "loc:dmlkZW9zLzEvMTA4MHAubXA0/q:75/webp/w:1920/sig:198acb42e0564c7f1023f903cd524c3355aceb2190036fad7d3e5eb70cf713a4"

	params, err := ParsePathParams(pathParams)
	if err != nil {
		t.Fatalf("ParsePathParams failed: %v", err)
	}

	// Verify parameters
	if params.Location != "dmlkZW9zLzEvMTA4MHAubXA0" {
		t.Errorf("Expected location 'dmlkZW9zLzEvMTA4MHAubXA0', got '%s'", params.Location)
	}
	if params.Quality != 75 {
		t.Errorf("Expected quality 75, got %d", params.Quality)
	}
	if params.Width != 1920 {
		t.Errorf("Expected width 1920, got %d", params.Width)
	}
	if !params.Webp {
		t.Error("Expected webp to be true")
	}
	if params.Signature != "198acb42e0564c7f1023f903cd524c3355aceb2190036fad7d3e5eb70cf713a4" {
		t.Errorf("Expected signature '198acb42e0564c7f1023f903cd524c3355aceb2190036fad7d3e5eb70cf713a4', got '%s'", params.Signature)
	}
	if params.EncodedURL != "" {
		t.Errorf("Expected no encoded URL, got '%s'", params.EncodedURL)
	}
}

func TestParsePathParams_WithEncodedURL(t *testing.T) {
	// Test case: Traditional format with encoded URL at the end
	pathParams := "q:75/webp/w:1920/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ"

	params, err := ParsePathParams(pathParams)
	if err != nil {
		t.Fatalf("ParsePathParams failed: %v", err)
	}

	// Verify parameters
	if params.Quality != 75 {
		t.Errorf("Expected quality 75, got %d", params.Quality)
	}
	if params.Width != 1920 {
		t.Errorf("Expected width 1920, got %d", params.Width)
	}
	if !params.Webp {
		t.Error("Expected webp to be true")
	}
	if params.EncodedURL != "aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ" {
		t.Errorf("Expected encoded URL 'aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ', got '%s'", params.EncodedURL)
	}
	if params.Location != "" {
		t.Errorf("Expected no location, got '%s'", params.Location)
	}
}

func TestParsePathParams_WithLocationAndURL(t *testing.T) {
	// Test case: Both location and URL (both should be parsed)
	pathParams := "loc:dmlkZW9z/q:50/sig:abc123/aHR0cHM6Ly9leGFtcGxl"

	params, err := ParsePathParams(pathParams)
	if err != nil {
		t.Fatalf("ParsePathParams failed: %v", err)
	}

	// When location is present AND last part doesn't look like a parameter, it's treated as URL
	if params.Location != "dmlkZW9z" {
		t.Errorf("Expected location 'dmlkZW9z', got '%s'", params.Location)
	}
	if params.Quality != 50 {
		t.Errorf("Expected quality 50, got %d", params.Quality)
	}
	if params.Signature != "abc123" {
		t.Errorf("Expected signature 'abc123', got '%s'", params.Signature)
	}
	// The last part should be treated as encoded URL since it doesn't contain ":"
	if params.EncodedURL != "aHR0cHM6Ly9leGFtcGxl" {
		t.Errorf("Expected encoded URL 'aHR0cHM6Ly9leGFtcGxl', got '%s'", params.EncodedURL)
	}
}
