package http

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"api/internal/model"
)

const (
	// MaxResponseSize limits response body to 50MB to prevent memory exhaustion
	MaxResponseSize = 50 * 1024 * 1024

	// Default timeout for HTTP requests
	DefaultTimeout = 30 * time.Second
)

// Options configures how a Client sends requests
type Options struct {
	Timeout          time.Duration // zero means DefaultTimeout
	NoFollowRedirect bool          // return 3xx responses instead of following them
	ProxyURL         string        // empty means use the environment's proxy settings
	Insecure         bool          // skip TLS certificate verification
}

// Client wraps the standard http.Client with additional functionality
type Client struct {
	client *http.Client
}

// NewClient creates a new HTTP client with default options
func NewClient() *Client {
	c, _ := NewClientWithOptions(Options{})
	return c
}

// NewClientWithOptions creates a new HTTP client configured by opts
func NewClientWithOptions(opts Options) (*Client, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Refuse to connect to cloud metadata addresses. Checking here, on the
	// address actually being dialed, also covers redirects, DNS names that
	// resolve to them and encoded forms like http://2852039166/.
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   blockMetadataAddress,
	}).DialContext
	if opts.ProxyURL != "" {
		proxy, err := url.Parse(opts.ProxyURL)
		if err != nil || proxy.Host == "" {
			return nil, fmt.Errorf("invalid proxy URL: %q", opts.ProxyURL)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	if opts.Insecure {
		fmt.Fprintln(os.Stderr, "WARNING: TLS certificate verification is disabled (--insecure).")
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{Timeout: timeout, Transport: transport}
	if opts.NoFollowRedirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return &Client{client: client}, nil
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
			// Join repeated headers (e.g. Set-Cookie) so no values are lost
			respHeaders[key] = strings.Join(values, ", ")
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

// Head performs a HEAD request
func (c *Client) Head(url string, headers map[string]string) (*model.Response, error) {
	return c.Do("HEAD", url, headers, "")
}

// Options performs an OPTIONS request
func (c *Client) Options(url string, headers map[string]string) (*model.Response, error) {
	return c.Do("OPTIONS", url, headers, "")
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

	// Warn about localhost and loopback addresses
	if isLoopbackHost(hostname) {
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

// parseHost parses an IP address host, handling IPv6 brackets and zones
func parseHost(hostname string) net.IP {
	h := strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")
	if i := strings.Index(h, "%"); i >= 0 {
		h = h[:i]
	}
	return net.ParseIP(h)
}

// isLoopbackHost reports whether the host is localhost or a loopback IP
func isLoopbackHost(hostname string) bool {
	lower := strings.ToLower(strings.TrimSuffix(hostname, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	ip := parseHost(hostname)
	return ip != nil && ip.IsLoopback()
}

// isPrivateOrReservedHost checks if the hostname is a private or reserved IP
// (RFC 1918, unique-local IPv6, link-local, or the unspecified 0.0.0.0/8 range).
// Host names are never considered private here.
func isPrivateOrReservedHost(hostname string) bool {
	ip := parseHost(hostname)
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 { // 0.0.0.0/8
			return true
		}
		ip = v4
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// metadataIPs are cloud metadata service addresses (common SSRF targets)
var metadataIPs = []net.IP{
	net.ParseIP("169.254.169.254"), // AWS, GCP, Azure metadata
	net.ParseIP("169.254.170.2"),   // AWS ECS task metadata
	net.ParseIP("100.100.100.200"), // Alibaba Cloud metadata
	net.ParseIP("fd00:ec2::254"),   // AWS metadata over IPv6
}

// isCloudMetadataEndpoint checks if the hostname is a cloud metadata service
func isCloudMetadataEndpoint(hostname string) bool {
	lower := strings.ToLower(strings.TrimSuffix(hostname, "."))
	if lower == "metadata.google.internal" || lower == "metadata.goog" {
		return true
	}
	if ip := parseHost(hostname); ip != nil {
		for _, m := range metadataIPs {
			if ip.Equal(m) { // Equal also matches IPv4-mapped IPv6 forms
				return true
			}
		}
	}
	return false
}

// blockMetadataAddress is a net.Dialer Control function that rejects
// connections to cloud metadata addresses.
func blockMetadataAddress(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if isCloudMetadataEndpoint(host) {
		return fmt.Errorf("blocked connection to cloud metadata endpoint: %s", host)
	}
	return nil
}
