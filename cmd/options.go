package cmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
	httpclient "api/internal/http"
	"api/internal/vars"
)

var (
	queryParams   []string
	formFields    []string
	multipartArgs []string
	bearerToken   string
	basicAuth     string
	timeoutSecs   float64
	noRedirect    bool
	proxyURL      string
	insecureTLS   bool
)

// addTransportFlags registers query, body-encoding, auth and connection flags
func addTransportFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringArrayVarP(&queryParams, "query", "q", nil, "Add query parameter key=value (can be used multiple times)")
	f.StringArrayVar(&formFields, "form", nil, "Send a URL-encoded form field key=value (can be used multiple times)")
	f.StringArrayVarP(&multipartArgs, "multipart", "F", nil, "Send a multipart field key=value, or key=@file for a file upload")
	f.StringVar(&bearerToken, "bearer", "", "Set Authorization: Bearer <token>")
	f.StringVar(&basicAuth, "basic", "", "Set HTTP basic auth as user:password")
	addConnectionFlags(cmd)
}

// addConnectionFlags registers timeout, redirect, proxy and TLS flags
func addConnectionFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Float64Var(&timeoutSecs, "timeout", 0, "Request timeout in seconds (default 30)")
	f.BoolVar(&noRedirect, "no-redirect", false, "Don't follow redirects")
	f.StringVar(&proxyURL, "proxy", "", "Proxy URL (default: from environment)")
	f.BoolVar(&insecureTLS, "insecure", false, "Skip TLS certificate verification")
}

// clientFromFlags builds an HTTP client from the connection flags
func clientFromFlags() (*httpclient.Client, error) {
	if timeoutSecs < 0 {
		return nil, fmt.Errorf("--timeout must not be negative")
	}
	return httpclient.NewClientWithOptions(httpclient.Options{
		Timeout:          time.Duration(timeoutSecs * float64(time.Second)),
		NoFollowRedirect: noRedirect,
		ProxyURL:         proxyURL,
		Insecure:         insecureTLS,
	})
}

// splitKeyValue splits "key=value" at the first '='
func splitKeyValue(s string) (string, string, error) {
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return "", "", fmt.Errorf("expected key=value, got %q", s)
	}
	return s[:idx], s[idx+1:], nil
}

// addQueryParams appends key=value pairs to the URL's query string, keeping
// their order. {{variable}} placeholders are left unescaped.
func addQueryParams(rawURL string, params []string) (string, error) {
	if len(params) == 0 {
		return rawURL, nil
	}
	base, fragment := rawURL, ""
	if i := strings.Index(base, "#"); i >= 0 {
		base, fragment = base[:i], base[i:]
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		k, v, err := splitKeyValue(p)
		if err != nil {
			return "", fmt.Errorf("--query: %w", err)
		}
		parts = append(parts, vars.MapSegments(k, url.QueryEscape)+"="+vars.MapSegments(v, url.QueryEscape))
	}
	sep := "?"
	switch {
	case strings.HasSuffix(base, "?") || strings.HasSuffix(base, "&"):
		sep = ""
	case strings.Contains(base, "?"):
		sep = "&"
	}
	return base + sep + strings.Join(parts, "&") + fragment, nil
}

// buildFormBody encodes key=value pairs as application/x-www-form-urlencoded,
// leaving {{variable}} placeholders unescaped
func buildFormBody(fields []string) (string, error) {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		k, v, err := splitKeyValue(f)
		if err != nil {
			return "", fmt.Errorf("--form: %w", err)
		}
		parts = append(parts, vars.MapSegments(k, url.QueryEscape)+"="+vars.MapSegments(v, url.QueryEscape))
	}
	return strings.Join(parts, "&"), nil
}

// buildMultipartBody builds a multipart/form-data body. Values starting with
// '@' are read from a file inside the current directory. It returns the body
// and the Content-Type header value (including the boundary).
func buildMultipartBody(fields []string) (string, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range fields {
		k, v, err := splitKeyValue(f)
		if err != nil {
			return "", "", fmt.Errorf("--multipart: %w", err)
		}
		if strings.HasPrefix(v, "@") {
			path := strings.TrimPrefix(v, "@")
			content, err := readBodyFromFile(path)
			if err != nil {
				return "", "", fmt.Errorf("--multipart: failed to read %s: %w", path, err)
			}
			name := path
			if i := strings.LastIndexAny(name, `/\`); i >= 0 {
				name = name[i+1:]
			}
			part, err := w.CreateFormFile(k, name)
			if err != nil {
				return "", "", err
			}
			part.Write([]byte(content))
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return "", "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", "", err
	}
	return buf.String(), w.FormDataContentType(), nil
}

// hasHeader reports whether the header map contains name (case-insensitive)
func hasHeader(headers map[string]string, name string) bool {
	for k := range headers {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// resolveAll applies resolve to every string in in
func resolveAll(in []string, resolve func(string) string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = resolve(v)
	}
	return out
}

// applyAuthAndBody applies --bearer/--basic and --form/--multipart to the
// request, passing every user-supplied value through resolve. Headers given
// explicitly with -H are never overridden.
func applyAuthAndBody(headers map[string]string, body string, resolve func(string) string) (string, error) {
	bearer, basic := resolve(bearerToken), resolve(basicAuth)
	if bearer != "" && basic != "" {
		return "", fmt.Errorf("--bearer and --basic cannot be used together")
	}
	if bearer != "" && !hasHeader(headers, "Authorization") {
		headers["Authorization"] = "Bearer " + bearer
	}
	if basic != "" && !hasHeader(headers, "Authorization") {
		if !strings.Contains(basic, ":") {
			return "", fmt.Errorf("--basic expects user:password")
		}
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(basic))
	}

	encodings := 0
	for _, set := range []bool{body != "", len(formFields) > 0, len(multipartArgs) > 0} {
		if set {
			encodings++
		}
	}
	if encodings > 1 {
		return "", fmt.Errorf("use only one of --data, --form and --multipart")
	}

	if len(formFields) > 0 {
		encoded, err := buildFormBody(resolveAll(formFields, resolve))
		if err != nil {
			return "", err
		}
		if !hasHeader(headers, "Content-Type") {
			headers["Content-Type"] = "application/x-www-form-urlencoded"
		}
		return encoded, nil
	}
	if len(multipartArgs) > 0 {
		encoded, contentType, err := buildMultipartBody(resolveAll(multipartArgs, resolve))
		if err != nil {
			return "", err
		}
		if !hasHeader(headers, "Content-Type") {
			headers["Content-Type"] = contentType
		}
		return encoded, nil
	}
	return body, nil
}
