package http

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vedsharma/apicli/internal/model"
)

// Client wraps the standard http.Client with additional functionality
type Client struct {
	client *http.Client
}

// NewClient creates a new HTTP client
func NewClient() *Client {
	return &Client{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Do executes an HTTP request and returns the response
func (c *Client) Do(method, url string, headers map[string]string, body string) (*model.Response, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
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

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
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
