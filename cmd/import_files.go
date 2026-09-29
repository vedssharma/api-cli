package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"api/internal/format"
	"api/internal/har"
	"api/internal/model"
	"api/internal/openapi"
	"api/internal/vars"
)

// maxImportSize bounds the size of files read by the import commands
const maxImportSize = 50 << 20

// registerImportFiles adds `import har` and `import openapi`
func registerImportFiles(importCmd *cobra.Command) {
	harCmd := &cobra.Command{
		Use:   "har <file>",
		Short: "Import requests from a HAR file (browser network log)",
		Long: `Import the requests in a HAR file, such as one saved from your browser's
network tab, into a collection. Browser-added headers (Host, Content-Length,
sec-*, ...) are dropped and sensitive headers (Authorization, Cookie, ...)
are redacted before saving. Entries that are not plain HTTP(S) requests
(e.g. WebSockets) are skipped.

Examples:
  apicli import har session.har
  apicli import har session.har --filter /api/ --collection my-api`,
		Args: cobra.ExactArgs(1),
		Run:  runImportHAR,
	}
	harCmd.Flags().StringP("collection", "c", "", "Target collection name (default: the file name)")
	harCmd.Flags().String("filter", "", "Only import requests whose URL contains this text")

	openapiCmd := &cobra.Command{
		Use:   "openapi <file>",
		Short: "Import operations from an OpenAPI 3 / Swagger 2 file (JSON or YAML)",
		Long: `Create one saved request per operation in an OpenAPI 3.x or Swagger 2.0
document (JSON or YAML).

  - Path parameters and required query/header parameters become {{variables}}.
  - Security schemes become placeholder credentials such as
    'Authorization: Bearer {{token}}' or '{{apiKey}}'.
  - JSON request bodies are sketched from the schema or example.
  - The first documented 2xx status becomes a 'status=' assertion, so
    'collection run' works as a smoke test.

The base URL comes from the first server (or host/basePath). If the document
has none, or you pass --base-url, requests use it; use --base-url '{{baseUrl}}'
to keep the host in an environment variable.

Examples:
  apicli import openapi petstore.yaml
  apicli import openapi api.json --collection api --base-url http://localhost:8080`,
		Args: cobra.ExactArgs(1),
		Run:  runImportOpenAPI,
	}
	openapiCmd.Flags().StringP("collection", "c", "", "Target collection name (default: the API title)")
	openapiCmd.Flags().String("base-url", "", "Base URL to use instead of the document's server")

	importCmd.AddCommand(harCmd, openapiCmd)
}

// readImportFile reads a file for import with a size limit
func readImportFile(path string) []byte {
	info, err := os.Stat(path)
	exitOnErr(err, "Failed to read file")
	if info.Size() > maxImportSize {
		format.PrintError(fmt.Sprintf("File is too large (%d bytes, limit %d)", info.Size(), maxImportSize))
		os.Exit(1)
	}
	data, err := os.ReadFile(path) // #nosec G304 – path comes from a CLI argument
	exitOnErr(err, "Failed to read file")
	return data
}

// saveImportedRequests redacts sensitive headers and appends the requests to a
// collection, reporting what was done.
func saveImportedRequests(collection string, requests []model.SavedRequest) {
	store := openStoreOrExit()
	defer store.Close()

	redacted := map[string]bool{}
	saved := 0
	for _, r := range requests {
		filtered := filterSensitiveHeaders(r.Headers)
		for k, v := range filtered {
			if v == redactedValue && r.Headers[k] != redactedValue {
				redacted[k] = true
			}
		}
		r.Headers = filtered
		if err := store.AddToCollection(collection, r); err != nil {
			format.PrintError(fmt.Sprintf("Failed to add request '%s': %v", r.Name, err))
			continue
		}
		saved++
	}

	if len(redacted) > 0 {
		names := make([]string, 0, len(redacted))
		for k := range redacted {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Fprintf(os.Stderr, "WARNING: redacted sensitive headers before saving: %s\n", strings.Join(names, ", "))
	}
	format.PrintSuccess(fmt.Sprintf("Imported %d/%d requests into collection '%s'", saved, len(requests), collection))
}

// variablesUsed lists the {{variables}} referenced by the requests
func variablesUsed(requests []model.SavedRequest) []string {
	seen := map[string]bool{}
	collect := func(s string) {
		_, missing := vars.Substitute(s, nil)
		for _, m := range missing {
			seen[m] = true
		}
	}
	for _, r := range requests {
		collect(r.URL)
		collect(r.Body)
		for k, v := range r.Headers {
			collect(k)
			collect(v)
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func runImportHAR(cmd *cobra.Command, args []string) {
	collection, _ := cmd.Flags().GetString("collection")
	filter, _ := cmd.Flags().GetString("filter")

	res, err := har.Convert(readImportFile(args[0]), filter)
	exitOnErr(err, "Failed to import HAR file")

	if collection == "" {
		base := filepath.Base(args[0])
		collection = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if res.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "Skipped %d non-HTTP(S) entries\n", res.Skipped)
	}
	saveImportedRequests(collection, res.Requests)
}

func runImportOpenAPI(cmd *cobra.Command, args []string) {
	collection, _ := cmd.Flags().GetString("collection")
	baseURL, _ := cmd.Flags().GetString("base-url")

	res, err := openapi.Convert(readImportFile(args[0]), openapi.Options{BaseURL: baseURL})
	exitOnErr(err, "Failed to import OpenAPI document")

	if collection == "" {
		collection = res.Title
	}
	if collection == "" {
		base := filepath.Base(args[0])
		collection = strings.TrimSuffix(base, filepath.Ext(base))
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
	}
	saveImportedRequests(collection, res.Requests)

	if used := variablesUsed(res.Requests); len(used) > 0 {
		fmt.Printf("Variables used: %s\n", strings.Join(used, ", "))
		fmt.Println("Set them with: apicli env create <env> && apicli env set <env> NAME=value ...")
	}
}
