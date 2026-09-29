package cmd

import (
	"fmt"
	"os"

	"api/internal/assert"
	"api/internal/format"
	httpclient "api/internal/http"
	"api/internal/model"
	"api/internal/storage"
	"api/internal/vars"
	"github.com/spf13/cobra"
)

var (
	assertFlags []string
	stopOnError bool
)

func init() {
	collectionCmd := &cobra.Command{
		Use:     "collection",
		Aliases: []string{"col"},
		Short:   "Manage request collections",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all collections",
		Run:   runCollectionList,
	}

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new collection",
		Args:  cobra.ExactArgs(1),
		Run:   runCollectionCreate,
	}

	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show requests in a collection",
		Args:  cobra.ExactArgs(1),
		Run:   runCollectionShow,
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a collection",
		Args:  cobra.ExactArgs(1),
		Run:   runCollectionDelete,
	}

	addCmd := &cobra.Command{
		Use:   "add <collection> <name> <method> <url>",
		Short: "Add a request to a collection",
		Long: `Add a request to a collection.

Example:
  apicli collection add my-api "Get Users" GET https://api.example.com/users`,
		Args: cobra.MinimumNArgs(4),
		Run:  runCollectionAdd,
	}
	addCmd.Flags().StringArrayVarP(&headers, "header", "H", []string{}, "Add header")
	addCmd.Flags().StringVarP(&data, "data", "d", "", "Request body")
	addCmd.Flags().StringArrayVar(&assertFlags, "assert", nil, "Assertion to check when the collection runs, e.g. status=200 (can be used multiple times)")

	runCmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Run all requests in a collection",
		Args:  cobra.ExactArgs(1),
		Run:   runCollectionRun,
	}

	runCmd.Flags().BoolVar(&failOnError, "fail", false, "Count responses with status 400 or higher as failures")
	runCmd.Flags().BoolVar(&stopOnError, "stop-on-error", false, "Stop at the first failed request or assertion")
	addConnectionFlags(runCmd)
	addVariableFlags(runCmd)
	collectionCmd.AddCommand(listCmd, createCmd, showCmd, deleteCmd, addCmd, runCmd)
	registerCollectionManageCommands(collectionCmd)
	rootCmd.AddCommand(collectionCmd)
}

func runCollectionList(cmd *cobra.Command, args []string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collections: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	collections, err := store.LoadCollections()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collections: %v", err))
		os.Exit(1)
	}

	format.PrintCollectionList(collections)
}

func runCollectionCreate(cmd *cobra.Command, args []string) {
	name := args[0]

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to create collection: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	if err := store.CreateCollection(name); err != nil {
		format.PrintError(fmt.Sprintf("Failed to create collection: %v", err))
		os.Exit(1)
	}

	format.PrintSuccess(fmt.Sprintf("Collection '%s' created", name))
}

func runCollectionShow(cmd *cobra.Command, args []string) {
	name := args[0]

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collection: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	col, err := store.GetCollection(name)
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collection: %v", err))
		os.Exit(1)
	}

	if col == nil {
		format.PrintError(fmt.Sprintf("Collection '%s' not found", name))
		os.Exit(1)
	}

	format.PrintCollectionRequests(col)
}

func runCollectionDelete(cmd *cobra.Command, args []string) {
	name := args[0]

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to delete collection: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	if err := store.DeleteCollection(name); err != nil {
		format.PrintError(fmt.Sprintf("Failed to delete collection: %v", err))
		os.Exit(1)
	}

	format.PrintSuccess(fmt.Sprintf("Collection '%s' deleted", name))
}

func runCollectionAdd(cmd *cobra.Command, args []string) {
	collectionName := args[0]
	requestName := args[1]
	method := args[2]
	url := args[3]

	headerMap := parseHeaders(headers)

	// Filter sensitive headers before storing in collection
	filteredHeaders := filterSensitiveHeaders(headerMap)

	if err := validateAssertions(assertFlags); err != nil {
		format.PrintError(err.Error())
		os.Exit(1)
	}

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to add request: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	req := model.SavedRequest{
		Name:       requestName,
		Method:     method,
		URL:        url,
		Headers:    filteredHeaders,
		Body:       data,
		Assertions: assertFlags,
	}

	if err := store.AddToCollection(collectionName, req); err != nil {
		format.PrintError(fmt.Sprintf("Failed to add request: %v", err))
		os.Exit(1)
	}

	format.PrintSuccess(fmt.Sprintf("Request '%s' added to collection '%s'", requestName, collectionName))
}

func runCollectionRun(cmd *cobra.Command, args []string) {
	name := args[0]
	verbose, _ := cmd.Flags().GetBool("verbose")

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collection: %v", err))
		os.Exit(1)
	}
	defer store.Close()

	col, err := store.GetCollection(name)
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collection: %v", err))
		os.Exit(1)
	}

	if col == nil {
		format.PrintError(fmt.Sprintf("Collection '%s' not found", name))
		os.Exit(1)
	}

	if len(col.Requests) == 0 {
		format.PrintError(fmt.Sprintf("Collection '%s' is empty", name))
		os.Exit(1)
	}

	client, err := clientFromFlags()
	if err != nil {
		format.PrintError(err.Error())
		os.Exit(1)
	}

	fmt.Printf("Running %d requests from collection '%s'\n\n", len(col.Requests), name)

	failures := 0

	varMap, err := loadVariables()
	if err != nil {
		format.PrintError(err.Error())
		os.Exit(1)
	}

	for i, req := range col.Requests {
		failed := runSavedRequest(client, req, i+1, len(col.Requests), varMap, verbose)
		if failed {
			failures++
			if stopOnError {
				if remaining := len(col.Requests) - i - 1; remaining > 0 {
					fmt.Printf("Stopping: skipped %d remaining request(s)\n\n", remaining)
				}
				break
			}
		}
	}

	if failures > 0 {
		format.PrintError(fmt.Sprintf("Completed collection '%s' with %d failed request(s)", name, failures))
		os.Exit(1)
	}

	format.PrintSuccess(fmt.Sprintf("Completed running collection '%s'", name))
}

// runSavedRequest sends one saved request, prints the result and checks its
// assertions. It reports whether the request failed.
func runSavedRequest(client *httpclient.Client, req model.SavedRequest, n, total int, varMap map[string]string, verbose bool) (failed bool) {
	// Substitute {{variables}}, then resolve alias if present
	resolver := vars.NewResolver(varMap)
	resolvedURL := resolveAlias(resolver.String(req.URL))
	reqHeaders := resolver.Map(req.Headers)
	reqBody := resolver.String(req.Body)
	assertions := resolveAll(req.Assertions, resolver.String)

	if req.Name != "" {
		fmt.Printf("[%d/%d] %s\n", n, total, req.Name)
	} else {
		fmt.Printf("[%d/%d] %s %s\n", n, total, req.Method, resolvedURL)
	}

	if err := resolver.Err(); err != nil {
		format.PrintError(fmt.Sprintf("Skipping request: %v", err))
		return true
	}

	resp, err := client.Do(req.Method, resolvedURL, reqHeaders, reqBody)
	if err != nil {
		format.PrintError(fmt.Sprintf("Request failed: %v", err))
		return true
	}

	format.PrintResponse(resp, verbose)

	if err := captureFromResponse(resp, varMap, false); err != nil {
		format.PrintError(err.Error())
		failed = true
	}
	if failOnError && resp.StatusCode >= 400 {
		failed = true
	}
	for _, expr := range assertions {
		a, err := assert.Parse(expr)
		if err != nil {
			format.PrintError(err.Error())
			failed = true
			continue
		}
		if ok, detail := a.Check(resp); ok {
			format.PrintSuccess(fmt.Sprintf("assert %s", a.Raw))
		} else {
			format.PrintError(fmt.Sprintf("assert %s (%s)", a.Raw, detail))
			failed = true
		}
	}
	fmt.Println()
	return failed
}

// validateAssertions checks that every assertion expression parses
func validateAssertions(exprs []string) error {
	for _, e := range exprs {
		if _, err := assert.Parse(e); err != nil {
			return err
		}
	}
	return nil
}
