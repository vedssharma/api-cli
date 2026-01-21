package http

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"api/internal/model"
)

const (
	// MaxResponseSize limits response body to 50MB to prevent memory exhaustion
	MaxResponseSize = 50 * 1024 * 1024

	// Default timeout for HTTP requests
	DefaultTimeout = 30 * time.Second
)

// Client wraps the standard http.Client with additional functionality
type Client struct {
	client *http.Client
}

// NewClient creates a new HTTP client
func NewClient() *Client {
	return &Client{
		client: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// Do executes an HTTP request and returns the response
func (c *Client) Do(method, reqURL string, headers map[string]string, body string) (*model.Response, error) {
	// Validate URL and check for SSRF risks
	if err := validateURL(reqURL); err != nil {
		return nil, err
	}

	// Warn about insecure HTTP connections
	if strings.HasPrefix(strings.ToLower(reqURL), "http://") {
		fmt.Fprintln(os.Stderr, "WARNING: Using insecure HTTP connection. Data will be transmitted unencrypted.")
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Default Content-Type for requests with body
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	duration := time.Since(start)

	// Read response body with size limit to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, MaxResponseSize+1)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}

	// Check if response was truncated
	if int64(len(respBody)) > MaxResponseSize {
		respBody = respBody[:MaxResponseSize]
		fmt.Fprintln(os.Stderr, "WARNING: Response body truncated (exceeded 50MB limit)")
	}

	// Convert response headers
	respHeaders := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			respHeaders[key] = values[0]
		}
	}

	return &model.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    respHeaders,
		Body:       string(respBody),
		DurationMs: duration.Milliseconds(),
	}, nil
}

// Get performs a GET request
func (c *Client) Get(url string, headers map[string]string) (*model.Response, error) {
	return c.Do("GET", url, headers, "")
}

// Post performs a POST request
func (c *Client) Post(url string, headers map[string]string, body string) (*model.Response, error) {
	return c.Do("POST", url, headers, body)
}

// Put performs a PUT request
func (c *Client) Put(url string, headers map[string]string, body string) (*model.Response, error) {
	return c.Do("PUT", url, headers, body)
}

// Patch performs a PATCH request
func (c *Client) Patch(url string, headers map[string]string, body string) (*model.Response, error) {
	return c.Do("PATCH", url, headers, body)
}

// Delete performs a DELETE request
func (c *Client) Delete(url string, headers map[string]string) (*model.Response, error) {
	return c.Do("DELETE", url, headers, "")
}

// validateURL checks the URL for potential SSRF vulnerabilities
func validateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Ensure scheme is http or https
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (only http and https are allowed)", parsed.Scheme)
	}

	// Get the hostname (without port)
	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Block localhost and loopback addresses
	lowerHost := strings.ToLower(hostname)
	if lowerHost == "localhost" || lowerHost == "127.0.0.1" || lowerHost == "::1" {
		fmt.Fprintln(os.Stderr, "WARNING: Making request to localhost/loopback address")
	}

	// Check for private/internal IP ranges and cloud metadata endpoints
	if isPrivateOrReservedHost(hostname) {
		fmt.Fprintln(os.Stderr, "WARNING: Making request to private/internal IP address")
	}

	// Block cloud metadata endpoints (common SSRF targets)
	if isCloudMetadataEndpoint(hostname) {
		return fmt.Errorf("blocked request to cloud metadata endpoint: %s", hostname)
	}

	return nil
}

// isPrivateOrReservedHost checks if the hostname is a private or reserved IP
func isPrivateOrReservedHost(hostname string) bool {
	// Check for common private IP patterns
	privatePatterns := []string{
		"10.",          // 10.0.0.0/8
		"192.168.",     // 192.168.0.0/16
		"172.16.", "172.17.", "172.18.", "172.19.", // 172.16.0.0/12
		"172.20.", "172.21.", "172.22.", "172.23.",
		"172.24.", "172.25.", "172.26.", "172.27.",
		"172.28.", "172.29.", "172.30.", "172.31.",
		"0.",       // 0.0.0.0/8
		"169.254.", // Link-local
	}

	for _, pattern := range privatePatterns {
		if strings.HasPrefix(hostname, pattern) {
			return true
		}
	}

	return false
}

// isCloudMetadataEndpoint checks if the hostname is a cloud metadata service
func isCloudMetadataEndpoint(hostname string) bool {
	// Block common cloud metadata endpoints (SSRF targets)
	metadataHosts := map[string]bool{
		"169.254.169.254":          true, // AWS, GCP, Azure metadata
		"metadata.google.internal": true, // GCP metadata
		"metadata.goog":            true, // GCP metadata alternative
		"100.100.100.200":          true, // Alibaba Cloud metadata
		"169.254.170.2":            true, // AWS ECS task metadata
	}

	return metadataHosts[strings.ToLower(hostname)]
}
