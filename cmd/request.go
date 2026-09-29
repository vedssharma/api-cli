package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"api/internal/format"
	"api/internal/vars"
	"api/internal/model"
	"api/internal/storage"
)

// sensitiveHeaders is a list of headers that should be redacted before storing in history
var sensitiveHeaders = map[string]bool{
	// Standard authentication headers
	"authorization":       true,
	"proxy-authorization": true,
	"www-authenticate":    true,

	// Session and token headers
	"cookie":       true,
	"set-cookie":   true,
	"x-api-key":    true,
	"api-key":      true,
	"x-auth-token": true,
	"x-csrf-token": true,
	"x-xsrf-token": true,

	// AWS credentials
	"x-amz-security-token": true,
	"x-amz-credential":     true,
	"x-amz-signature":      true,

	// GCP credentials
	"x-goog-authenticated-user-email": true,
	"x-goog-authenticated-user-id":    true,
	"x-goog-iap-jwt-assertion":        true,

	// Azure credentials
	"x-ms-client-principal":    true,
	"x-ms-client-principal-id": true,
	"x-ms-token-aad-id-token":  true,

	// Other common auth headers
	"x-access-token":  true,
	"x-refresh-token": true,
	"x-session-token": true,
	"x-secret-key":    true,
	"x-private-key":   true,
}

var (
	headers     []string
	data        string
	noHistory   bool
	saveToCollection string
	rawOutput   bool
	silent      bool
	failOnError bool
	outputFile  string
	selectPath  string
)

func init() {
	// GET command
	getCmd := &cobra.Command{
		Use:   "get <url>",
		Short: "Send a GET request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("GET"),
	}
	addRequestFlags(getCmd)
	rootCmd.AddCommand(getCmd)

	// POST command
	postCmd := &cobra.Command{
		Use:   "post <url>",
		Short: "Send a POST request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("POST"),
	}
	addRequestFlags(postCmd)
	rootCmd.AddCommand(postCmd)

	// PUT command
	putCmd := &cobra.Command{
		Use:   "put <url>",
		Short: "Send a PUT request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("PUT"),
	}
	addRequestFlags(putCmd)
	rootCmd.AddCommand(putCmd)

	// PATCH command
	patchCmd := &cobra.Command{
		Use:   "patch <url>",
		Short: "Send a PATCH request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("PATCH"),
	}
	addRequestFlags(patchCmd)
	rootCmd.AddCommand(patchCmd)

	// HEAD command
	headCmd := &cobra.Command{
		Use:   "head <url>",
		Short: "Send a HEAD request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("HEAD"),
	}
	addRequestFlags(headCmd)
	rootCmd.AddCommand(headCmd)

	// OPTIONS command
	optionsCmd := &cobra.Command{
		Use:   "options <url>",
		Short: "Send an OPTIONS request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("OPTIONS"),
	}
	addRequestFlags(optionsCmd)
	rootCmd.AddCommand(optionsCmd)

	// DELETE command
	deleteCmd := &cobra.Command{
		Use:   "delete <url>",
		Short: "Send a DELETE request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("DELETE"),
	}
	addRequestFlags(deleteCmd)
	rootCmd.AddCommand(deleteCmd)
}

func addRequestFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(&headers, "header", "H", []string{}, "Add header (can be used multiple times)")
	cmd.Flags().StringVarP(&data, "data", "d", "", "Request body (JSON string or @filename)")
	cmd.Flags().BoolVar(&noHistory, "no-history", false, "Don't save to history")
	cmd.Flags().StringVarP(&saveToCollection, "collection", "c", "", "Save to collection")
	cmd.Flags().BoolVar(&rawOutput, "raw", false, "Print only the response body, unformatted")
	cmd.Flags().BoolVarP(&silent, "silent", "s", false, "Suppress response output")
	cmd.Flags().BoolVar(&failOnError, "fail", false, "Exit with status 22 if the response status is 400 or higher")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write the response body to a file")
	addTransportFlags(cmd)
	addVariableFlags(cmd)
	cmd.Flags().StringVar(&selectPath, "select", "", "Print only the JSON value at a path, e.g. .data.items[0].name")
}

func runRequest(method string) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		url := args[0]
		verbose, _ := cmd.Flags().GetBool("verbose")

		// Read body from file if prefixed with @
		rawBody := data
		if strings.HasPrefix(rawBody, "@") {
			filename := strings.TrimPrefix(rawBody, "@")
			content, err := readBodyFromFile(filename)
			if err != nil {
				format.PrintError(fmt.Sprintf("Failed to read file: %v", err))
				os.Exit(1)
			}
			rawBody = content
		}

		// Build the request twice: once keeping {{variables}} as written (what
		// gets stored in history/collections, so variable values never leak
		// there) and once with variables substituted (what gets sent).
		stored, err := buildRequest(url, rawBody, identity)
		if err != nil {
			format.PrintError(err.Error())
			os.Exit(1)
		}

		varMap, err := loadVariables()
		if err != nil {
			format.PrintError(err.Error())
			os.Exit(1)
		}
		resolver := vars.NewResolver(varMap)
		sent, err := buildRequest(url, rawBody, resolver.String)
		if err == nil {
			err = resolver.Err()
		}
		if err != nil {
			format.PrintError(err.Error())
			os.Exit(1)
		}

		// Warn if body contains potentially sensitive data
		if !noHistory {
			warnIfSensitiveBody(stored.body)
		}

		// Create HTTP client and make request
		client, err := clientFromFlags()
		if err != nil {
			format.PrintError(err.Error())
			os.Exit(1)
		}
		resp, err := client.Do(method, sent.url, sent.headers, sent.body)
		if err != nil {
			format.PrintError(fmt.Sprintf("Request failed: %v", err))
			os.Exit(1)
		}

		// Capture response values into variables
		if err := captureFromResponse(resp, varMap, true); err != nil {
			format.PrintError(err.Error())
		}

		// Print response
		if err := outputResponse(resp, verbose); err != nil {
			format.PrintError(err.Error())
			os.Exit(1)
		}

		// Save to history unless disabled
		if !noHistory {
			saveToHistory(method, stored.url, stored.headers, stored.body, resp)
		}

		// Save to collection if specified
		if saveToCollection != "" {
			saveRequestToCollection(saveToCollection, method, stored.url, stored.headers, stored.body)
		}

		if failOnError && resp.StatusCode >= 400 {
			os.Exit(failExitCode)
		}
	}
}

// failExitCode is the exit status used by --fail, matching curl's --fail.
const failExitCode = 22

// outputResponse writes the response according to the output flags.
func outputResponse(resp *model.Response, verbose bool) error {
	body := resp.Body
	if selectPath != "" {
		selected, err := format.SelectJSON(body, selectPath)
		if err != nil {
			return fmt.Errorf("--select: %v", err)
		}
		body = selected
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(body), 0600); err != nil {
			return fmt.Errorf("failed to write output file: %v", err)
		}
	}

	switch {
	case silent:
	case outputFile != "":
		format.PrintStatus(resp)
	case rawOutput || selectPath != "":
		format.PrintRawBody(body)
	default:
		format.PrintResponse(resp, verbose)
	}
	return nil
}

type builtRequest struct {
	url     string
	headers map[string]string
	body    string
}

func identity(s string) string { return s }

// buildRequest assembles the final URL, headers and body from the request
// flags, passing every user-supplied value through resolve.
func buildRequest(rawURL, rawBody string, resolve func(string) string) (*builtRequest, error) {
	u, err := addQueryParams(resolveAlias(resolve(rawURL)), resolveAll(queryParams, resolve))
	if err != nil {
		return nil, err
	}
	headerMap := parseHeaders(resolveAll(headers, resolve))
	body, err := applyAuthAndBody(headerMap, resolve(rawBody), resolve)
	if err != nil {
		return nil, err
	}
	return &builtRequest{url: u, headers: headerMap, body: body}, nil
}

func parseHeaders(headerStrings []string) map[string]string {
	result := make(map[string]string)
	for _, h := range headerStrings {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result[key] = value
		}
	}
	return result
}

func saveToHistory(method, url string, headers map[string]string, body string, resp *model.Response) {
	store, err := storage.NewStorage()
	if err != nil {
		// Silently fail - don't interrupt the user
		return
	}

	// Filter sensitive headers before storing
	filteredHeaders := filterSensitiveHeaders(headers)

	// Filter sensitive response headers if present
	var filteredResp *model.Response
	if resp != nil {
		filteredResp = &model.Response{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Headers:    filterSensitiveHeaders(resp.Headers),
			Body:       resp.Body,
			DurationMs: resp.DurationMs,
		}
	}

	req := model.Request{
		ID:        uuid.New().String()[:8],
		Timestamp: time.Now(),
		Method:    method,
		URL:       url,
		Headers:   filteredHeaders,
		Body:      body,
		Response:  filteredResp,
	}

	_ = store.AddToHistory(req)
}

func saveRequestToCollection(collectionName, method, url string, headers map[string]string, body string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to save to collection: %v", err))
		return
	}

	req := model.SavedRequest{
		Name:    "",
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    body,
	}

	if err := store.AddToCollection(collectionName, req); err != nil {
		format.PrintError(fmt.Sprintf("Failed to save to collection: %v", err))
		return
	}

	format.PrintSuccess(fmt.Sprintf("Saved to collection '%s'", collectionName))
}

// resolveAlias resolves URL aliases to their full URLs.
// If the URL starts with http:// or https://, it's returned as-is.
// Otherwise, it checks if the first path segment is a known alias.
func resolveAlias(url string) string {
	// Skip if already a full URL
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}

	// Split on first / to get potential alias name and path
	var aliasName, path string
	if idx := strings.Index(url, "/"); idx != -1 {
		aliasName = url[:idx]
		path = url[idx+1:]
	} else {
		// No path component, the whole URL is potentially an alias
		aliasName = url
		path = ""
	}

	// Try to resolve the alias
	store, err := storage.NewStorage()
	if err != nil {
		// Storage error, return URL as-is
		return url
	}

	baseURL, exists, err := store.GetAlias(aliasName)
	if err != nil || !exists {
		// Alias not found or error, return URL as-is
		return url
	}

	// Combine base URL with path (auto-normalize trailing slashes)
	baseURL = strings.TrimSuffix(baseURL, "/")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		return baseURL
	}
	return baseURL + "/" + path
}

// readBodyFromFile reads file content with path validation to prevent directory traversal
func readBodyFromFile(filename string) (string, error) {
	// Get working directory
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Get absolute path of the requested file
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}

	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(absPath)

	// Ensure file is within working directory (prevent path traversal)
	if !strings.HasPrefix(cleanPath, wd+string(filepath.Separator)) && cleanPath != wd {
		return "", fmt.Errorf("access denied: file must be within current directory")
	}

	// Check for symlinks - resolve and verify target is also within working directory
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		// If file doesn't exist, we'll let ReadFile handle the error
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to resolve path: %w", err)
		}
		realPath = cleanPath
	} else {
		// Verify symlink target is also within working directory
		if !strings.HasPrefix(realPath, wd+string(filepath.Separator)) && realPath != wd {
			return "", fmt.Errorf("access denied: symlink target must be within current directory")
		}
	}

	content, err := os.ReadFile(realPath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// filterSensitiveHeaders returns a copy of headers with sensitive values redacted
func filterSensitiveHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	filtered := make(map[string]string)
	for k, v := range headers {
		if sensitiveHeaders[strings.ToLower(k)] {
			filtered[k] = "[REDACTED]"
		} else {
			filtered[k] = v
		}
	}
	return filtered
}

// sensitiveBodyPatterns contains patterns that suggest sensitive data in request bodies
var sensitiveBodyPatterns = []string{
	"password", "passwd", "pwd",
	"secret", "token", "api_key", "apikey",
	"private_key", "privatekey",
	"credit_card", "creditcard", "card_number",
	"ssn", "social_security",
	"access_token", "refresh_token",
	"client_secret", "auth",
}

// warnIfSensitiveBody checks if the request body might contain sensitive data and warns the user
func warnIfSensitiveBody(body string) {
	if body == "" {
		return
	}

	lowerBody := strings.ToLower(body)
	for _, pattern := range sensitiveBodyPatterns {
		if strings.Contains(lowerBody, pattern) {
			fmt.Fprintln(os.Stderr, "WARNING: Request body may contain sensitive data (e.g., passwords, tokens). This will be stored in history.")
			fmt.Fprintln(os.Stderr, "         Use --no-history flag to skip storing this request.")
			return
		}
	}
}
