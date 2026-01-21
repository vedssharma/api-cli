package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"api/internal/format"
	httpclient "api/internal/http"
	"api/internal/model"
	"api/internal/storage"
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

	runCmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Run all requests in a collection",
		Args:  cobra.ExactArgs(1),
		Run:   runCollectionRun,
	}

	collectionCmd.AddCommand(listCmd, createCmd, showCmd, deleteCmd, addCmd, runCmd)
	rootCmd.AddCommand(collectionCmd)
}

func runCollectionList(cmd *cobra.Command, args []string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collections: %v", err))
		os.Exit(1)
	}

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

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to add request: %v", err))
		os.Exit(1)
	}

	req := model.SavedRequest{
		Name:    requestName,
		Method:  method,
		URL:     url,
		Headers: filteredHeaders,
		Body:    data,
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

	client := httpclient.NewClient()

	fmt.Printf("Running %d requests from collection '%s'\n\n", len(col.Requests), name)

	for i, req := range col.Requests {
		// Resolve alias if present
		resolvedURL := resolveAlias(req.URL)

		if req.Name != "" {
			fmt.Printf("[%d/%d] %s\n", i+1, len(col.Requests), req.Name)
		} else {
			fmt.Printf("[%d/%d] %s %s\n", i+1, len(col.Requests), req.Method, resolvedURL)
		}

		resp, err := client.Do(req.Method, resolvedURL, req.Headers, req.Body)
		if err != nil {
			format.PrintError(fmt.Sprintf("Request failed: %v", err))
			continue
		}

		format.PrintResponse(resp, verbose)
		fmt.Println()
	}

	format.PrintSuccess(fmt.Sprintf("Completed running collection '%s'", name))
}
