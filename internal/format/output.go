package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/fatih/color"
	"api/internal/model"
)

// sanitizeOutput removes or escapes potentially dangerous control characters
// that could manipulate terminal display or execute commands
func sanitizeOutput(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			// Allow common whitespace characters
			result.WriteRune(r)
		case r == '\x1b':
			// Escape ANSI escape sequences - replace ESC with visible representation
			result.WriteString("\\x1b")
		case unicode.IsControl(r) && r < 0x20:
			// Replace other control characters (0x00-0x1F except allowed whitespace)
			result.WriteString(fmt.Sprintf("\\x%02x", r))
		case r == 0x7F:
			// DEL character
			result.WriteString("\\x7f")
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}

var (
	successColor = color.New(color.FgGreen, color.Bold)
	redirectColor = color.New(color.FgYellow, color.Bold)
	clientErrColor = color.New(color.FgRed, color.Bold)
	serverErrColor = color.New(color.FgRed, color.Bold, color.BgWhite)
	headerKeyColor = color.New(color.FgCyan)
	methodColor = color.New(color.FgMagenta, color.Bold)
	urlColor = color.New(color.FgBlue)
	dimColor = color.New(color.Faint)
)

// PrintResponse prints a formatted HTTP response
func PrintResponse(resp *model.Response, showHeaders bool) {
	// Print status line with color based on status code
	printStatusLine(resp)

	// Print duration
	dimColor.Printf("  Time: %dms\n\n", resp.DurationMs)

	// Print headers if requested
	if showHeaders {
		printHeaders(resp.Headers)
	}

	// Print body
	printBody(resp.Body)
}

func printStatusLine(resp *model.Response) {
	statusColor := getStatusColor(resp.StatusCode)
	statusColor.Printf("%s\n", sanitizeOutput(resp.Status))
}

func getStatusColor(code int) *color.Color {
	switch {
	case code >= 200 && code < 300:
		return successColor
	case code >= 300 && code < 400:
		return redirectColor
	case code >= 400 && code < 500:
		return clientErrColor
	default:
		return serverErrColor
	}
}

func printHeaders(headers map[string]string) {
	if len(headers) == 0 {
		return
	}

	fmt.Println("Headers:")

	// Sort headers for consistent output
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		headerKeyColor.Printf("  %s: ", sanitizeOutput(key))
		fmt.Println(sanitizeOutput(headers[key]))
	}
	fmt.Println()
}

func printBody(body string) {
	if body == "" {
		dimColor.Println("(empty body)")
		return
	}

	// Try to pretty-print JSON, then sanitize output for terminal safety
	prettyBody := prettyJSON(body)
	fmt.Println(sanitizeOutput(prettyBody))
}

func prettyJSON(s string) string {
	var out bytes.Buffer
	err := json.Indent(&out, []byte(s), "", "  ")
	if err != nil {
		// Not valid JSON, return as-is
		return s
	}
	return out.String()
}

// PrintRequest prints a formatted HTTP request summary
func PrintRequest(req *model.Request) {
	methodColor.Printf("%s ", req.Method)
	urlColor.Println(sanitizeOutput(req.URL))
	dimColor.Printf("  ID: %s\n", req.ID)
	dimColor.Printf("  Time: %s\n", req.Timestamp.Format("2006-01-02 15:04:05"))

	if req.Response != nil {
		fmt.Print("  Status: ")
		statusColor := getStatusColor(req.Response.StatusCode)
		statusColor.Println(sanitizeOutput(req.Response.Status))
	}
}

// PrintRequestDetail prints full request/response details
func PrintRequestDetail(req *model.Request) {
	fmt.Println("Request:")
	fmt.Println(strings.Repeat("-", 40))
	methodColor.Printf("%s ", req.Method)
	urlColor.Println(sanitizeOutput(req.URL))
	dimColor.Printf("ID: %s\n", req.ID)
	dimColor.Printf("Time: %s\n\n", req.Timestamp.Format("2006-01-02 15:04:05"))

	if len(req.Headers) > 0 {
		printHeaders(req.Headers)
	}

	if req.Body != "" {
		fmt.Println("Body:")
		fmt.Println(sanitizeOutput(prettyJSON(req.Body)))
		fmt.Println()
	}

	if req.Response != nil {
		fmt.Println("\nResponse:")
		fmt.Println(strings.Repeat("-", 40))
		PrintResponse(req.Response, true)
	}
}

// PrintHistoryList prints a list of requests in a compact format
func PrintHistoryList(requests []model.Request, limit int) {
	if len(requests) == 0 {
		dimColor.Println("No requests in history")
		return
	}

	count := len(requests)
	if limit > 0 && limit < count {
		count = limit
	}

	for i := 0; i < count; i++ {
		req := requests[i]
		dimColor.Printf("[%d] ", i+1)
		methodColor.Printf("%-7s ", req.Method)

		// Truncate URL if too long, then sanitize
		url := req.URL
		if len(url) > 60 {
			url = url[:57] + "..."
		}
		urlColor.Printf("%-60s ", sanitizeOutput(url))

		if req.Response != nil {
			statusColor := getStatusColor(req.Response.StatusCode)
			statusColor.Printf("%d ", req.Response.StatusCode)
			dimColor.Printf("(%dms)", req.Response.DurationMs)
		}
		fmt.Println()
	}

	if limit > 0 && len(requests) > limit {
		dimColor.Printf("\n... and %d more requests\n", len(requests)-limit)
	}
}

// PrintCollectionList prints a list of collections
func PrintCollectionList(collections *model.Collections) {
	if len(collections.Collections) == 0 {
		dimColor.Println("No collections found")
		return
	}

	fmt.Println("Collections:")
	for name, col := range collections.Collections {
		headerKeyColor.Printf("  %s ", sanitizeOutput(name))
		dimColor.Printf("(%d requests)\n", len(col.Requests))
	}
}

// PrintCollectionRequests prints requests in a collection
func PrintCollectionRequests(col *model.Collection) {
	if len(col.Requests) == 0 {
		dimColor.Printf("Collection '%s' is empty\n", sanitizeOutput(col.Name))
		return
	}

	headerKeyColor.Printf("Collection: %s\n", sanitizeOutput(col.Name))
	fmt.Println(strings.Repeat("-", 40))

	for i, req := range col.Requests {
		dimColor.Printf("[%d] ", i+1)
		if req.Name != "" {
			fmt.Printf("%s: ", sanitizeOutput(req.Name))
		}
		methodColor.Printf("%s ", req.Method)
		urlColor.Println(sanitizeOutput(req.URL))
	}
}

// PrintSuccess prints a success message
func PrintSuccess(msg string) {
	successColor.Printf("✓ %s\n", msg)
}

// PrintError prints an error message
func PrintError(msg string) {
	clientErrColor.Printf("✗ %s\n", msg)
}

// PrintAliasList prints a list of aliases
func PrintAliasList(aliases *model.Aliases) {
	if len(aliases.Aliases) == 0 {
		dimColor.Println("No aliases found")
		return
	}

	fmt.Println("Aliases:")
	for name, url := range aliases.Aliases {
		headerKeyColor.Printf("  %s ", sanitizeOutput(name))
		dimColor.Print("→ ")
		urlColor.Println(sanitizeOutput(url))
	}
}

// PrintAlias prints a single alias
func PrintAlias(name, url string) {
	headerKeyColor.Printf("%s ", sanitizeOutput(name))
	dimColor.Print("→ ")
	urlColor.Println(sanitizeOutput(url))
}
