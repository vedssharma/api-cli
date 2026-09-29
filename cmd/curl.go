package cmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"api/internal/curl"
	"api/internal/format"
	"api/internal/model"
	"api/internal/storage"
)

var printCurl bool

// registerImportCurl adds `import curl`
func registerImportCurl(importCmd *cobra.Command) {
	curlImportCmd := &cobra.Command{
		Use:   "curl <command>",
		Short: "Import a curl command as a saved request",
		Long: `Convert a curl command into a request saved in a collection.

Pass the whole command as one quoted argument, paste it unquoted after
'curl', or use - to read it from standard input. Put apicli's own flags
(-c, -n) before the command; everything after it belongs to curl. Options
apicli cannot represent (such as -F uploads) are reported and skipped.

Sensitive headers (Authorization, Cookie, API keys) are redacted before
saving, as with 'collection add'. Replace them with a {{variable}} and
'apicli env set' the value.

Examples:
  apicli import curl "curl -X POST https://api.example.com/users -d '{\"name\":\"Jo\"}'"
  apicli import curl -c my-api curl https://api.example.com/users -H 'Accept: text/plain'
  pbpaste | apicli import curl -`,
		Args: cobra.MinimumNArgs(1),
		Run:  runImportCurl,
	}
	curlImportCmd.Flags().StringP("collection", "c", "curl", "Target collection name")
	curlImportCmd.Flags().StringP("name", "n", "", "Request name (default: METHOD /path)")
	// Stop parsing apicli flags at the first argument so curl's own options
	// (-H, --header, -d ...) pass through untouched.
	curlImportCmd.Flags().SetInterspersed(false)
	importCmd.AddCommand(curlImportCmd)
}

// curlTokens turns the command arguments into curl tokens. When the command
// is a single quoted argument (or - for stdin), apicli flags may follow it, so
// the remaining arguments are parsed as flags.
func curlTokens(cmd *cobra.Command, args []string) ([]string, error) {
	first := args[0]
	if first != "-" && !strings.ContainsAny(first, " \t\n") {
		return args, nil // unquoted: every argument belongs to curl
	}

	text := first
	if first == "-" {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			return nil, err
		}
		text = string(b)
	}
	if len(args) > 1 {
		if err := cmd.Flags().Parse(args[1:]); err != nil {
			return nil, err
		}
		if extra := cmd.Flags().Args(); len(extra) > 0 {
			return nil, fmt.Errorf("unexpected arguments after the quoted command: %s", strings.Join(extra, " "))
		}
	}
	return curl.Tokenize(text)
}

// requestNameFromURL builds a default request name such as "GET /users"
func requestNameFromURL(method, rawURL string) string {
	path := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		path = u.Path
		if path == "" {
			path = "/"
		}
	}
	return method + " " + path
}

func runImportCurl(cmd *cobra.Command, args []string) {
	tokens, err := curlTokens(cmd, args)
	exitOnErr(err, "Failed to read curl command")
	collectionName, _ := cmd.Flags().GetString("collection")
	name, _ := cmd.Flags().GetString("name")
	parsed, err := curl.Parse(tokens)
	exitOnErr(err, "Failed to parse curl command")

	for _, w := range parsed.Warnings {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
	}

	saved := filterSensitiveHeaders(parsed.Headers)
	var redacted []string
	for k, v := range saved {
		if v == redactedValue && parsed.Headers[k] != redactedValue {
			redacted = append(redacted, k)
		}
	}
	sort.Strings(redacted)

	if name == "" {
		name = requestNameFromURL(parsed.Method, parsed.URL)
	}
	store := openStoreOrExit()
	defer store.Close()
	exitOnErr(store.AddToCollection(collectionName, model.SavedRequest{
		Name: name, Method: parsed.Method, URL: parsed.URL, Headers: saved, Body: parsed.Body,
	}), "Failed to save request")

	if len(redacted) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: redacted before saving: %s. Use a {{variable}} to keep them replayable.\n",
			strings.Join(redacted, ", "))
	}
	format.PrintSuccess(fmt.Sprintf("Imported '%s' into collection '%s'", name, collectionName))
}

// registerExportCurl adds `export curl`
func registerExportCurl(exportCmd *cobra.Command) {
	curlExportCmd := &cobra.Command{
		Use:   "curl",
		Short: "Export requests as curl commands",
		Long: `Export saved requests as curl commands, one per request.

By default all collections are exported. Use --collection for one collection
or --history for request history. Redacted headers and {{variable}}
placeholders are written as stored, so fill them in before running.

Examples:
  apicli export curl --collection my-api
  apicli export curl --history --output replay.sh`,
		Run: runExportCurl,
	}
	curlExportCmd.Flags().StringP("collection", "c", "", "Export a specific collection by name")
	curlExportCmd.Flags().Bool("history", false, "Export request history instead of collections")
	curlExportCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")
	exportCmd.AddCommand(curlExportCmd)
}

type curlExportItem struct {
	label   string
	request model.SavedRequest
}

func runExportCurl(cmd *cobra.Command, args []string) {
	collectionName, _ := cmd.Flags().GetString("collection")
	exportHistory, _ := cmd.Flags().GetBool("history")
	outputPath, _ := cmd.Flags().GetString("output")

	store := openStoreOrExit()
	defer store.Close()

	items, err := collectCurlItems(store, collectionName, exportHistory)
	exitOnErr(err, "Failed to load requests")
	if len(items) == 0 {
		format.PrintError("No requests to export")
		os.Exit(1)
	}

	text := renderCurlExport(items)
	if outputPath != "" {
		exitOnErr(os.WriteFile(outputPath, []byte(text), 0600), "Failed to write file")
		format.PrintSuccess(fmt.Sprintf("Wrote %d curl command(s) to %s", len(items), outputPath))
		return
	}
	fmt.Print(text)
}

func collectCurlItems(store *storage.SQLiteStorage, collectionName string, history bool) ([]curlExportItem, error) {
	var items []curlExportItem
	switch {
	case history:
		h, err := store.LoadHistory()
		if err != nil {
			return nil, err
		}
		for _, r := range h.Requests {
			items = append(items, curlExportItem{
				label:   r.ID,
				request: model.SavedRequest{Method: r.Method, URL: r.URL, Headers: r.Headers, Body: r.Body},
			})
		}
	case collectionName != "":
		col, err := store.GetCollection(collectionName)
		if err != nil {
			return nil, err
		}
		if col == nil {
			return nil, fmt.Errorf("collection '%s' not found", collectionName)
		}
		for _, r := range col.Requests {
			items = append(items, curlExportItem{label: r.Name, request: r})
		}
	default:
		all, err := store.LoadCollections()
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(all.Collections))
		for n := range all.Collections {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			for _, r := range all.Collections[n].Requests {
				label := r.Name
				if label == "" {
					label = r.Method + " " + r.URL
				}
				items = append(items, curlExportItem{label: n + " / " + label, request: r})
			}
		}
	}
	return items, nil
}

// renderCurlExport writes each request as a commented curl command
func renderCurlExport(items []curlExportItem) string {
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		if it.label != "" {
			// Keep the label on one line so it can't break out of the comment
			b.WriteString("# " + strings.NewReplacer("\n", " ", "\r", " ").Replace(it.label) + "\n")
		}
		r := it.request
		b.WriteString(curl.Build(r.Method, r.URL, r.Headers, r.Body) + "\n")
	}
	return b.String()
}
