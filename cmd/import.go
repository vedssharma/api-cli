package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"api/internal/format"
	"api/internal/model"
	"api/internal/storage"

	"github.com/spf13/cobra"
)

// Import-specific types that handle Postman's polymorphic fields.
// The URL field can be either a JSON string or an object, and items
// can be either leaf requests or folders containing more items.

type postmanCollectionImport struct {
	Info  postmanInfo         `json:"info"`
	Items []postmanItemImport `json:"item"`
}

type postmanItemImport struct {
	Name    string                `json:"name"`
	Request *postmanRequestImport `json:"request,omitempty"` // nil when item is a folder
	Items   []postmanItemImport   `json:"item,omitempty"`    // non-empty when item is a folder
}

// postmanRequestImport uses json.RawMessage for URL because Postman
// collections allow the url field to be either a plain string or an object.
type postmanRequestImport struct {
	Method string          `json:"method"`
	Header []postmanHeader `json:"header"`
	URL    json.RawMessage `json:"url"`
	Body   *postmanBody    `json:"body,omitempty"`
}

func init() {
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import data from external formats",
	}

	postmanImportCmd := &cobra.Command{
		Use:   "postman <file>",
		Short: "Import a Postman Collection JSON file into apicli",
		Long: `Import a Postman Collection v2.0 or v2.1 JSON file and save all requests
as an apicli collection so they can be run, edited, or exported.

Nested folders are flattened and each folder name is prepended to the
request name so nothing is lost (e.g. "Auth / Login").

By default the collection is named after the Postman collection. Use
--collection to choose a different name. If a collection with that name
already exists the imported requests are appended to it.

Examples:
  apicli import postman my-api.postman_collection.json
  apicli import postman my-api.postman_collection.json --collection staging`,
		Args: cobra.ExactArgs(1),
		Run:  runImportPostman,
	}

	postmanImportCmd.Flags().StringP("collection", "c", "", "Target collection name (default: Postman collection name)")

	importCmd.AddCommand(postmanImportCmd)
	registerImportCurl(importCmd)
	registerImportFiles(importCmd)
	rootCmd.AddCommand(importCmd)
}

func runImportPostman(cmd *cobra.Command, args []string) {
	filePath := args[0]
	targetCollection, _ := cmd.Flags().GetString("collection")

	// Read and parse the Postman collection file
	data, err := os.ReadFile(filePath) // #nosec G304 – path comes from CLI argument, not user-controlled input
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to read file: %v", err))
		os.Exit(1)
	}

	var pc postmanCollectionImport
	if err := json.Unmarshal(data, &pc); err != nil {
		format.PrintError(fmt.Sprintf("Failed to parse Postman collection: %v", err))
		os.Exit(1)
	}

	// Determine the target collection name
	collectionName := pc.Info.Name
	if targetCollection != "" {
		collectionName = targetCollection
	}
	if collectionName == "" {
		collectionName = "Imported Collection"
	}

	// Flatten all items (including nested folders) into SavedRequests
	requests := flattenPostmanItems(pc.Items, "")
	if len(requests) == 0 {
		format.PrintError("No requests found in the Postman collection")
		os.Exit(1)
	}

	// Save to storage
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to open storage: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	// Create the collection (no-op if it already exists)
	if err := store.CreateCollection(collectionName); err != nil {
		format.PrintError(fmt.Sprintf("Failed to create collection: %v", err))
		os.Exit(1)
	}

	// Add each request
	imported := 0
	for _, req := range requests {
		if err := store.AddToCollection(collectionName, req); err != nil {
			format.PrintError(fmt.Sprintf("Failed to add request '%s': %v", req.Name, err))
			continue
		}
		imported++
	}

	format.PrintSuccess(fmt.Sprintf(
		"Imported %d/%d requests into collection '%s'",
		imported, len(requests), collectionName,
	))
}

// flattenPostmanItems recursively walks the item tree, turning folders into a
// name prefix (e.g. "Auth / ") so that all requests end up in a single flat list.
func flattenPostmanItems(items []postmanItemImport, prefix string) []model.SavedRequest {
	var result []model.SavedRequest

	for _, item := range items {
		if item.Request != nil {
			// Leaf request
			name := prefix + item.Name
			req := convertPostmanRequest(name, item.Request)
			result = append(result, req)
		} else if len(item.Items) > 0 {
			// Folder – recurse with updated prefix
			folderPrefix := prefix + item.Name + " / "
			result = append(result, flattenPostmanItems(item.Items, folderPrefix)...)
		}
	}

	return result
}

// convertPostmanRequest converts a Postman request into the apicli SavedRequest model.
func convertPostmanRequest(name string, pr *postmanRequestImport) model.SavedRequest {
	// Resolve URL from either a plain string or a structured object
	rawURL := resolvePostmanURL(pr.URL)

	// Convert header array to map, skipping disabled headers
	headers := make(map[string]string, len(pr.Header))
	for _, h := range pr.Header {
		if h.Key != "" {
			headers[h.Key] = h.Value
		}
	}

	// Extract body text (only "raw" mode is supported; other modes are skipped)
	body := ""
	if pr.Body != nil && strings.EqualFold(pr.Body.Mode, "raw") {
		body = pr.Body.Raw
	}

	return model.SavedRequest{
		Name:    name,
		Method:  strings.ToUpper(pr.Method),
		URL:     rawURL,
		Headers: headers,
		Body:    body,
	}
}

// resolvePostmanURL extracts a usable URL string from the raw JSON value.
// Postman allows the url field to be either a plain string or an object with
// a "raw" field (and optional structured parts).
func resolvePostmanURL(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try plain string first
	var urlStr string
	if err := json.Unmarshal(raw, &urlStr); err == nil {
		return urlStr
	}

	// Try structured object
	var obj struct {
		Raw      string   `json:"raw"`
		Protocol string   `json:"protocol"`
		Host     []string `json:"host"`
		Path     []string `json:"path"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}

	// Prefer the pre-built raw string if present
	if obj.Raw != "" {
		return obj.Raw
	}

	// Reconstruct a URL from parts as a fallback
	if len(obj.Host) > 0 {
		scheme := obj.Protocol
		if scheme == "" {
			scheme = "https"
		}
		host := strings.Join(obj.Host, ".")
		path := ""
		if len(obj.Path) > 0 {
			path = "/" + strings.Join(obj.Path, "/")
		}
		return fmt.Sprintf("%s://%s%s", scheme, host, path)
	}

	return ""
}
