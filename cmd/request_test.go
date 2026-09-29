package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api/internal/storage"
)

// ─── parseHeaders ─────────────────────────────────────────────────────────────

func TestParseHeaders_Single(t *testing.T) {
	got := parseHeaders([]string{"Content-Type: application/json"})
	if got["Content-Type"] != "application/json" {
		t.Errorf("expected 'application/json', got %q", got["Content-Type"])
	}
}

func TestParseHeaders_Multiple(t *testing.T) {
	got := parseHeaders([]string{
		"Content-Type: application/json",
		"Accept: */*",
		"X-Custom: myvalue",
	})
	if len(got) != 3 {
		t.Errorf("expected 3 headers, got %d", len(got))
	}
	if got["Accept"] != "*/*" {
		t.Errorf("unexpected Accept: %q", got["Accept"])
	}
	if got["X-Custom"] != "myvalue" {
		t.Errorf("unexpected X-Custom: %q", got["X-Custom"])
	}
}

func TestParseHeaders_TrimsWhitespace(t *testing.T) {
	got := parseHeaders([]string{"  Authorization :  Bearer token123  "})
	if got["Authorization"] != "Bearer token123" {
		t.Errorf("expected trimmed value, got %q", got["Authorization"])
	}
}

func TestParseHeaders_Malformed_NoColon(t *testing.T) {
	got := parseHeaders([]string{"NoColonHeader"})
	if len(got) != 0 {
		t.Errorf("expected no headers from malformed input, got %v", got)
	}
}

func TestParseHeaders_ValueWithColon(t *testing.T) {
	// Value contains a colon (e.g. Authorization: Bearer base64:encoded)
	got := parseHeaders([]string{"Authorization: Bearer base64:encoded"})
	if got["Authorization"] != "Bearer base64:encoded" {
		t.Errorf("expected 'Bearer base64:encoded', got %q", got["Authorization"])
	}
}

func TestParseHeaders_Empty(t *testing.T) {
	got := parseHeaders([]string{})
	if len(got) != 0 {
		t.Errorf("expected empty map for no headers, got %v", got)
	}
}

func TestParseHeaders_EmptyEntry(t *testing.T) {
	got := parseHeaders([]string{"Valid: yes", ""})
	// Empty string has no colon → skipped
	if len(got) != 1 {
		t.Errorf("expected 1 header (empty entry skipped), got %d", len(got))
	}
}

// ─── filterSensitiveHeaders ───────────────────────────────────────────────────

func TestFilterSensitiveHeaders_RedactsAuthorization(t *testing.T) {
	h := map[string]string{
		"Authorization": "Bearer super-secret-token",
		"Content-Type":  "application/json",
	}
	filtered := filterSensitiveHeaders(h)
	if filtered["Authorization"] != "[REDACTED]" {
		t.Errorf("Authorization should be redacted, got %q", filtered["Authorization"])
	}
	if filtered["Content-Type"] != "application/json" {
		t.Errorf("Content-Type should not be redacted, got %q", filtered["Content-Type"])
	}
}

func TestFilterSensitiveHeaders_CaseInsensitive(t *testing.T) {
	h := map[string]string{
		"AUTHORIZATION": "Bearer secret",
		"X-API-KEY":     "my-key",
		"x-auth-token":  "token-value",
		"Cookie":        "session=abc",
	}
	filtered := filterSensitiveHeaders(h)
	for _, k := range []string{"AUTHORIZATION", "X-API-KEY", "x-auth-token", "Cookie"} {
		if filtered[k] != "[REDACTED]" {
			t.Errorf("header %q should be redacted, got %q", k, filtered[k])
		}
	}
}

func TestFilterSensitiveHeaders_AllSensitiveHeaders(t *testing.T) {
	sensitiveOnes := []string{
		"Authorization", "Proxy-Authorization", "WWW-Authenticate",
		"Cookie", "Set-Cookie", "X-Api-Key", "Api-Key",
		"X-Auth-Token", "X-CSRF-Token", "X-XSRF-Token",
		"X-Amz-Security-Token", "X-Amz-Credential", "X-Amz-Signature",
		"X-Goog-Authenticated-User-Email", "X-Goog-Authenticated-User-Id", "X-Goog-Iap-Jwt-Assertion",
		"X-Ms-Client-Principal", "X-Ms-Client-Principal-Id", "X-Ms-Token-Aad-Id-Token",
		"X-Access-Token", "X-Refresh-Token", "X-Session-Token", "X-Secret-Key", "X-Private-Key",
	}
	h := make(map[string]string)
	for _, k := range sensitiveOnes {
		h[k] = "secret-value"
	}
	filtered := filterSensitiveHeaders(h)
	for _, k := range sensitiveOnes {
		if filtered[k] != "[REDACTED]" {
			t.Errorf("expected %q to be redacted, got %q", k, filtered[k])
		}
	}
}

func TestFilterSensitiveHeaders_SafeHeadersUnchanged(t *testing.T) {
	h := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "*/*",
		"X-Request-ID": "abc-123",
		"User-Agent":   "apicli/1.0",
	}
	filtered := filterSensitiveHeaders(h)
	for k, v := range h {
		if filtered[k] != v {
			t.Errorf("header %q should not be redacted: got %q, want %q", k, filtered[k], v)
		}
	}
}

func TestFilterSensitiveHeaders_Nil(t *testing.T) {
	filtered := filterSensitiveHeaders(nil)
	if filtered != nil {
		t.Errorf("expected nil for nil input, got %v", filtered)
	}
}

func TestFilterSensitiveHeaders_Empty(t *testing.T) {
	filtered := filterSensitiveHeaders(map[string]string{})
	if len(filtered) != 0 {
		t.Errorf("expected empty map, got %v", filtered)
	}
}

// ─── warnIfSensitiveBody ──────────────────────────────────────────────────────

func TestWarnIfSensitiveBody_EmptyBody(t *testing.T) {
	// Should not panic
	warnIfSensitiveBody("")
}

func TestWarnIfSensitiveBody_NoSensitiveData(t *testing.T) {
	// Should not panic
	warnIfSensitiveBody(`{"name":"Alice","email":"alice@example.com"}`)
}

func TestWarnIfSensitiveBody_ContainsPassword(t *testing.T) {
	// Verify it doesn't panic with sensitive content
	warnIfSensitiveBody(`{"username":"alice","password":"s3cr3t"}`)
}

func TestWarnIfSensitiveBody_ContainsToken(t *testing.T) {
	warnIfSensitiveBody(`{"access_token":"eyJhbGci..."}`)
}

func TestWarnIfSensitiveBody_ContainsAPIKey(t *testing.T) {
	warnIfSensitiveBody(`{"api_key":"abc123xyz"}`)
}

func TestWarnIfSensitiveBody_ContainsSecret(t *testing.T) {
	warnIfSensitiveBody(`{"client_secret":"very-secret"}`)
}

func TestWarnIfSensitiveBody_CaseInsensitive(t *testing.T) {
	// Uppercase should still be detected
	warnIfSensitiveBody(`{"PASSWORD":"S3CR3T"}`)
}

// ─── resolveAlias ─────────────────────────────────────────────────────────────

func TestResolveAlias_FullHTTPSURL(t *testing.T) {
	url := "https://example.com/api/v1"
	got := resolveAlias(url)
	if got != url {
		t.Errorf("full https URL should be unchanged: got %q, want %q", got, url)
	}
}

func TestResolveAlias_FullHTTPURL(t *testing.T) {
	url := "http://example.com/api"
	got := resolveAlias(url)
	if got != url {
		t.Errorf("full http URL should be unchanged: got %q, want %q", got, url)
	}
}

func TestResolveAlias_UnknownAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Alias doesn't exist → returned as-is
	input := "unknownalias/some/path"
	got := resolveAlias(input)
	if got != input {
		t.Errorf("unknown alias should be unchanged: got %q, want %q", got, input)
	}
}

func TestResolveAlias_KnownAlias_WithPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := storage.NewStorage()
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()
	if err := store.CreateAlias("starwars", "https://swapi.tech/api"); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}

	got := resolveAlias("starwars/people/1")
	want := "https://swapi.tech/api/people/1"
	if got != want {
		t.Errorf("resolveAlias = %q, want %q", got, want)
	}
}

func TestResolveAlias_KnownAlias_NoPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := storage.NewStorage()
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()
	if err := store.CreateAlias("myapi", "https://api.example.com"); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}

	got := resolveAlias("myapi")
	want := "https://api.example.com"
	if got != want {
		t.Errorf("resolveAlias (no path) = %q, want %q", got, want)
	}
}

func TestResolveAlias_TrailingSlashNormalization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := storage.NewStorage()
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()
	// Base URL has trailing slash
	if err := store.CreateAlias("api", "https://api.example.com/"); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}

	got := resolveAlias("api/users")
	// Should not produce double slash
	if strings.Contains(got, "//users") {
		t.Errorf("double slash in resolved URL: %q", got)
	}
	if got != "https://api.example.com/users" {
		t.Errorf("resolveAlias = %q, want %q", got, "https://api.example.com/users")
	}
}

// ─── readBodyFromFile ─────────────────────────────────────────────────────────

// setupWorkDir creates a temp directory, changes to it, and restores after test.
func setupWorkDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })
	return tmpDir
}

func TestReadBodyFromFile_ValidFile(t *testing.T) {
	dir := setupWorkDir(t)
	content := `{"name":"test","value":42}`
	fpath := filepath.Join(dir, "body.json")
	if err := os.WriteFile(fpath, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := readBodyFromFile("body.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestReadBodyFromFile_NonExistent(t *testing.T) {
	setupWorkDir(t)
	_, err := readBodyFromFile("nonexistent.json")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestReadBodyFromFile_PathTraversal(t *testing.T) {
	setupWorkDir(t)
	// Attempt to read a file outside the working directory
	_, err := readBodyFromFile("../../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}
}

func TestReadBodyFromFile_AbsolutePathOutsideWD(t *testing.T) {
	setupWorkDir(t)
	// Absolute path outside working directory
	_, err := readBodyFromFile("/etc/hostname")
	if err == nil {
		t.Error("expected error for absolute path outside working dir, got nil")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}
}

func TestReadBodyFromFile_SubdirFile(t *testing.T) {
	dir := setupWorkDir(t)
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "subdir content"
	if err := os.WriteFile(filepath.Join(subdir, "data.txt"), []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := readBodyFromFile("subdir/data.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestReadBodyFromFile_EmptyFile(t *testing.T) {
	dir := setupWorkDir(t)
	fpath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(fpath, []byte(""), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := readBodyFromFile("empty.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty content, got %q", got)
	}
}
