package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"api/internal/format"
	"api/internal/model"
	"api/internal/storage"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// Postman Collection v2.1 structures
type postmanCollection struct {
	Info  postmanInfo   `json:"info"`
	Items []postmanItem `json:"item"`
}

type postmanInfo struct {
	Name      string `json:"name"`
	PostmanID string `json:"_postman_id"`
	Schema    string `json:"schema"`
}

type postmanItem struct {
	Name    string         `json:"name"`
	Request postmanRequest `json:"request"`
}

type postmanRequest struct {
	Method string          `json:"method"`
	Header []postmanHeader `json:"header"`
	URL    postmanURL      `json:"url"`
	Body   *postmanBody    `json:"body,omitempty"`
}

type postmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type postmanURL struct {
	Raw      string              `json:"raw"`
	Protocol string              `json:"protocol"`
	Host     []string            `json:"host"`
	Path     []string            `json:"path"`
	Query    []postmanQueryParam `json:"query,omitempty"`
}

type postmanQueryParam struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type postmanBody struct {
	Mode    string           `json:"mode"`
	Raw     string           `json:"raw"`
	Options *postmanBodyOpts `json:"options,omitempty"`
}

type postmanBodyOpts struct {
	Raw postmanBodyRawOpts `json:"raw"`
}

type postmanBodyRawOpts struct {
	Language string `json:"language"`
}

func init() {
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export data to external formats",
	}

	postmanCmd := &cobra.Command{
		Use:   "postman",
		Short: "Export to a Postman Collection v2.1 JSON file",
		Long: `Export requests to a Postman Collection v2.1 JSON file that can be imported into Postman.

By default, all collections are exported. Use --collection to export a specific
collection, or --history to export your request history instead.

Examples:
  apicli export postman
  apicli export postman --collection my-api
  apicli export postman --history
  apicli export postman --collection my-api --output my-api.postman.json`,
		Run: runExportPostman,
	}

	postmanCmd.Flags().StringP("collection", "c", "", "Export a specific collection by name")
	postmanCmd.Flags().BoolP("history", "", false, "Export request history instead of collections")
	postmanCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")

	exportCmd.AddCommand(postmanCmd)
	registerExportCurl(exportCmd)
	rootCmd.AddCommand(exportCmd)
}

func runExportPostman(cmd *cobra.Command, args []string) {
	collectionName, _ := cmd.Flags().GetString("collection")
	exportHistory, _ := cmd.Flags().GetBool("history")
	outputFile, _ := cmd.Flags().GetString("output")

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to open storage: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	var pc postmanCollection

	if exportHistory {
		pc, err = buildPostmanFromHistory(store)
		if err != nil {
			format.PrintError(fmt.Sprintf("Failed to load history: %v", err))
			os.Exit(1)
		}
	} else if collectionName != "" {
		pc, err = buildPostmanFromCollection(store, collectionName)
		if err != nil {
			format.PrintError(fmt.Sprintf("Failed to load collection: %v", err))
			os.Exit(1)
		}
	} else {
		pc, err = buildPostmanFromAllCollections(store)
		if err != nil {
			format.PrintError(fmt.Sprintf("Failed to load collections: %v", err))
			os.Exit(1)
		}
	}

	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to encode Postman collection: %v", err))
		os.Exit(1)
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, data, 0600); err != nil {
			format.PrintError(fmt.Sprintf("Failed to write file: %v", err))
			os.Exit(1)
		}
		format.PrintSuccess(fmt.Sprintf("Postman collection written to %s", outputFile))
	} else {
		fmt.Println(string(data))
	}
}

func buildPostmanFromCollection(store *storage.SQLiteStorage, name string) (postmanCollection, error) {
	col, err := store.GetCollection(name)
	if err != nil {
		return postmanCollection{}, err
	}
	if col == nil {
		return postmanCollection{}, fmt.Errorf("collection '%s' not found", name)
	}

	items := make([]postmanItem, 0, len(col.Requests))
	for _, req := range col.Requests {
		items = append(items, savedRequestToPostmanItem(req))
	}

	return postmanCollection{
		Info: postmanInfo{
			Name:      col.Name,
			PostmanID: uuid.New().String(),
			Schema:    "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Items: items,
	}, nil
}

func buildPostmanFromAllCollections(store *storage.SQLiteStorage) (postmanCollection, error) {
	collections, err := store.LoadCollections()
	if err != nil {
		return postmanCollection{}, err
	}

	items := make([]postmanItem, 0)
	for _, col := range collections.Collections {
		for _, req := range col.Requests {
			items = append(items, savedRequestToPostmanItem(req))
		}
	}

	name := "apicli Export"
	if len(collections.Collections) == 1 {
		for _, col := range collections.Collections {
			name = col.Name
		}
	}

	return postmanCollection{
		Info: postmanInfo{
			Name:      name,
			PostmanID: uuid.New().String(),
			Schema:    "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Items: items,
	}, nil
}

func buildPostmanFromHistory(store *storage.SQLiteStorage) (postmanCollection, error) {
	history, err := store.LoadHistory()
	if err != nil {
		return postmanCollection{}, err
	}

	items := make([]postmanItem, 0, len(history.Requests))
	for _, req := range history.Requests {
		items = append(items, historyRequestToPostmanItem(req))
	}

	return postmanCollection{
		Info: postmanInfo{
			Name:      fmt.Sprintf("apicli History Export %s", time.Now().Format("2006-01-02")),
			PostmanID: uuid.New().String(),
			Schema:    "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Items: items,
	}, nil
}

func savedRequestToPostmanItem(req model.SavedRequest) postmanItem {
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("%s %s", req.Method, req.URL)
	}
	return postmanItem{
		Name:    name,
		Request: buildPostmanRequest(req.Method, req.URL, req.Headers, req.Body),
	}
}

func historyRequestToPostmanItem(req model.Request) postmanItem {
	name := fmt.Sprintf("%s %s", req.Method, req.URL)
	return postmanItem{
		Name:    name,
		Request: buildPostmanRequest(req.Method, req.URL, req.Headers, req.Body),
	}
}

func buildPostmanRequest(method, rawURL string, headers map[string]string, body string) postmanRequest {
	pHeaders := make([]postmanHeader, 0, len(headers))
	for k, v := range headers {
		pHeaders = append(pHeaders, postmanHeader{Key: k, Value: v})
	}

	pURL := parsePostmanURL(rawURL)

	var pBody *postmanBody
	if body != "" {
		lang := "text"
		if isJSON(body) {
			lang = "json"
		}
		pBody = &postmanBody{
			Mode: "raw",
			Raw:  body,
			Options: &postmanBodyOpts{
				Raw: postmanBodyRawOpts{Language: lang},
			},
		}
	}

	return postmanRequest{
		Method: strings.ToUpper(method),
		Header: pHeaders,
		URL:    pURL,
		Body:   pBody,
	}
}

func parsePostmanURL(rawURL string) postmanURL {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		// Unparseable URL - return minimal representation
		return postmanURL{Raw: rawURL, Host: []string{rawURL}}
	}

	protocol := parsed.Scheme

	// Split host into parts (e.g. "api.example.com" -> ["api", "example", "com"])
	host := strings.Split(parsed.Hostname(), ".")

	// Split path into segments (strip leading slash, filter empty)
	pathStr := strings.TrimPrefix(parsed.Path, "/")
	var pathParts []string
	if pathStr != "" {
		for _, seg := range strings.Split(pathStr, "/") {
			if seg != "" {
				pathParts = append(pathParts, seg)
			}
		}
	}

	// Parse query params
	var query []postmanQueryParam
	for k, vals := range parsed.Query() {
		for _, v := range vals {
			query = append(query, postmanQueryParam{Key: k, Value: v})
		}
	}

	return postmanURL{
		Raw:      rawURL,
		Protocol: protocol,
		Host:     host,
		Path:     pathParts,
		Query:    query,
	}
}

func isJSON(s string) bool {
	s = strings.TrimSpace(s)
	return (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) ||
		(strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"))
}
