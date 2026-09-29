package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── validateURL ────────────────────────────────────────────────────────────

func TestValidateURL_ValidHTTPS(t *testing.T) {
	if err := validateURL("https://example.com/api/v1"); err != nil {
		t.Errorf("unexpected error for valid https URL: %v", err)
	}
}

func TestValidateURL_ValidHTTP(t *testing.T) {
	if err := validateURL("http://example.com/api"); err != nil {
		t.Errorf("unexpected error for valid http URL: %v", err)
	}
}

func TestValidateURL_UnsupportedScheme(t *testing.T) {
	cases := []string{"ftp://example.com", "file:///etc/passwd", "ssh://host.com", "data:text/plain,hello"}
	for _, u := range cases {
		if err := validateURL(u); err == nil {
			t.Errorf("expected error for scheme in %q, got nil", u)
		} else if !strings.Contains(err.Error(), "unsupported URL scheme") {
			t.Errorf("expected 'unsupported URL scheme' error for %q, got: %v", u, err)
		}
	}
}

func TestValidateURL_NoScheme(t *testing.T) {
	// A URL without a scheme gets an empty scheme, which is neither http nor https.
	if err := validateURL("example.com/path"); err == nil {
		t.Error("expected error for URL without scheme, got nil")
	}
}

func TestValidateURL_EmptyString(t *testing.T) {
	if err := validateURL(""); err == nil {
		t.Error("expected error for empty URL, got nil")
	}
}

func TestValidateURL_CloudMetadataAWS(t *testing.T) {
	err := validateURL("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected error for AWS metadata endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "blocked request to cloud metadata endpoint") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateURL_CloudMetadataGCP(t *testing.T) {
	err := validateURL("https://metadata.google.internal/computeMetadata/v1/")
	if err == nil {
		t.Fatal("expected error for GCP metadata endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "blocked request to cloud metadata endpoint") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateURL_CloudMetadataGoog(t *testing.T) {
	err := validateURL("https://metadata.goog/")
	if err == nil {
		t.Fatal("expected error for metadata.goog endpoint, got nil")
	}
}

func TestValidateURL_CloudMetadataAlibaba(t *testing.T) {
	err := validateURL("http://100.100.100.200/")
	if err == nil {
		t.Fatal("expected error for Alibaba Cloud metadata endpoint, got nil")
	}
}

func TestValidateURL_CloudMetadataECS(t *testing.T) {
	err := validateURL("http://169.254.170.2/")
	if err == nil {
		t.Fatal("expected error for AWS ECS metadata endpoint, got nil")
	}
}

// Localhost and private IPs should warn but NOT return errors.
func TestValidateURL_LocalhostNoError(t *testing.T) {
	if err := validateURL("http://localhost/"); err != nil {
		t.Errorf("unexpected error for localhost: %v", err)
	}
}

func TestValidateURL_LoopbackNoError(t *testing.T) {
	if err := validateURL("http://127.0.0.1/"); err != nil {
		t.Errorf("unexpected error for 127.0.0.1: %v", err)
	}
}

func TestValidateURL_PrivateIPNoError(t *testing.T) {
	for _, u := range []string{
		"http://10.0.0.1/",
		"http://192.168.1.100/",
		"http://172.16.0.1/",
	} {
		if err := validateURL(u); err != nil {
			t.Errorf("unexpected error for private IP %q: %v", u, err)
		}
	}
}

// ─── isPrivateOrReservedHost ────────────────────────────────────────────────

func TestIsPrivateOrReservedHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		// Private RFC-1918 ranges
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"192.168.0.1", true},
		{"192.168.1.100", true},
		{"172.16.0.1", true},
		{"172.20.0.1", true},
		{"172.31.255.255", true},
		// Link-local
		{"169.254.1.1", true},
		{"169.254.0.0", true},
		// 0.x
		{"0.0.0.1", true},
		// Outside private ranges
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"93.184.216.34", false},
		{"example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := isPrivateOrReservedHost(tt.host)
			if got != tt.want {
				t.Errorf("isPrivateOrReservedHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// ─── isCloudMetadataEndpoint ────────────────────────────────────────────────

func TestIsCloudMetadataEndpoint(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"169.254.169.254", true},
		{"metadata.google.internal", true},
		{"metadata.goog", true},
		{"100.100.100.200", true},
		{"169.254.170.2", true},
		// Case-insensitive
		{"METADATA.GOOGLE.INTERNAL", true},
		{"169.254.169.254", true},
		// Non-metadata
		{"example.com", false},
		{"8.8.8.8", false},
		{"169.254.169.253", false},
		{"100.100.100.201", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := isCloudMetadataEndpoint(tt.host)
			if got != tt.want {
				t.Errorf("isCloudMetadataEndpoint(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// ─── HTTP Client (httptest) ──────────────────────────────────────────────────

func TestClient_GET_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Get(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Body != `{"status":"ok"}` {
		t.Errorf("unexpected body: %q", resp.Body)
	}
}

func TestClient_POST_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Default Content-Type should be set by the client
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Post(srv.URL, nil, `{"name":"test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestClient_POST_ContentTypeNotOverridden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "text/plain" {
			t.Errorf("expected Content-Type text/plain, got %q", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	_, err := c.Post(srv.URL, map[string]string{"Content-Type": "text/plain"}, "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_PUT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Put(srv.URL, nil, `{"key":"value"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestClient_PATCH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Patch(srv.URL, nil, `{"field":"updated"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestClient_DELETE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Delete(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 204 {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestClient_CustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "secret123" {
			t.Errorf("expected X-Api-Key secret123, got %q", r.Header.Get("X-Api-Key"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("expected Accept application/json, got %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	_, err := c.Get(srv.URL, map[string]string{
		"X-Api-Key": "secret123",
		"Accept":    "application/json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_ResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "response-value")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Get(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers["X-Custom-Header"] != "response-value" {
		t.Errorf("expected response header 'response-value', got %q", resp.Headers["X-Custom-Header"])
	}
}

func TestClient_404Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Get(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestClient_DurationRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Get(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", resp.DurationMs)
	}
}

func TestClient_InvalidSchemeError(t *testing.T) {
	c := NewClient()
	_, err := c.Get("ftp://example.com", nil)
	if err == nil {
		t.Fatal("expected error for ftp scheme, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_CloudMetadataBlocked(t *testing.T) {
	c := NewClient()
	_, err := c.Get("http://169.254.169.254/latest/meta-data/", nil)
	if err == nil {
		t.Fatal("expected error for cloud metadata URL, got nil")
	}
	if !strings.Contains(err.Error(), "blocked request to cloud metadata endpoint") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_EmptyBody_NoContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When there's no body, Content-Type should not be set by the client
		if ct := r.Header.Get("Content-Type"); ct != "" {
			t.Errorf("expected no Content-Type for empty body, got %q", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	_, err := c.Get(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_StatusLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient()
	resp, err := c.Get(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Status, "401") {
		t.Errorf("expected Status to contain '401', got %q", resp.Status)
	}
}

func TestClient_HeadOptionsAndMultiValueHeaders(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
	}))
	defer srv.Close()

	c := NewClient()
	if _, err := c.Head(srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	resp, err := c.Options(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0] != "HEAD" || methods[1] != "OPTIONS" {
		t.Errorf("unexpected methods: %v", methods)
	}
	if got := resp.Headers["Set-Cookie"]; got != "a=1, b=2" {
		t.Errorf("Set-Cookie = %q, want both values", got)
	}
}
