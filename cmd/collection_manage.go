package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"api/internal/format"
	"api/internal/model"
	"api/internal/storage"
	"github.com/spf13/cobra"
)

// registerCollectionManageCommands adds the rename, remove-request and edit subcommands
func registerCollectionManageCommands(collectionCmd *cobra.Command) {
	renameCmd := &cobra.Command{
		Use:   "rename <old-name> <new-name>",
		Short: "Rename a collection",
		Args:  cobra.ExactArgs(2),
		Run:   runCollectionRename,
	}

	removeCmd := &cobra.Command{
		Use:     "remove-request <collection> <index or name>",
		Aliases: []string{"rm"},
		Short:   "Remove a request from a collection",
		Long: `Remove a request from a collection, identified by its position (as shown
by 'collection show', starting at 1) or its exact name.`,
		Args: cobra.ExactArgs(2),
		Run:  runCollectionRemoveRequest,
	}

	editCmd := &cobra.Command{
		Use:   "edit <collection> <index or name>",
		Short: "Edit a saved request",
		Long: `Edit a saved request, identified by its position (starting at 1) or exact name.
Only the fields you pass are changed. Headers are merged into the existing
ones; give "Name:" with an empty value to remove a header.

Example:
  apicli collection edit api 2 --url '{{base}}/v2/users' -H 'Accept: text/plain'`,
		Args: cobra.ExactArgs(2),
		Run:  runCollectionEdit,
	}
	editCmd.Flags().String("name", "", "New request name")
	editCmd.Flags().String("method", "", "New HTTP method")
	editCmd.Flags().String("url", "", "New URL")
	editCmd.Flags().StringArrayVarP(&headers, "header", "H", []string{}, "Add, replace or (with empty value) remove a header")
	editCmd.Flags().StringVarP(&data, "data", "d", "", "New request body (use -d '' to clear)")

	editCmd.Flags().StringArrayVar(&assertFlags, "assert", nil, "Replace the request's assertions (can be used multiple times)")
	editCmd.Flags().Bool("clear-assert", false, "Remove all of the request's assertions")

	collectionCmd.AddCommand(renameCmd, removeCmd, editCmd)
}

// findRequestIndex resolves a 1-based position or an exact name to a 0-based index
func findRequestIndex(col *model.Collection, ref string) (int, error) {
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 1 || n > len(col.Requests) {
			return 0, fmt.Errorf("request %d not found in collection '%s' (it has %d)", n, col.Name, len(col.Requests))
		}
		return n - 1, nil
	}

	match := -1
	for i, r := range col.Requests {
		if r.Name == ref {
			if match >= 0 {
				return 0, fmt.Errorf("several requests are named '%s'; use a position instead", ref)
			}
			match = i
		}
	}
	if match < 0 {
		return 0, fmt.Errorf("no request named '%s' in collection '%s'", ref, col.Name)
	}
	return match, nil
}

// loadCollectionOrExit opens storage and loads the named collection
func loadCollectionOrExit(name string) (*storage.SQLiteStorage, *model.Collection) {
	store := openStoreOrExit()
	col, err := store.GetCollection(name)
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load collection: %v", err))
		os.Exit(1)
	}
	if col == nil {
		format.PrintError(fmt.Sprintf("Collection '%s' not found", name))
		os.Exit(1)
	}
	return store, col
}

func runCollectionRename(cmd *cobra.Command, args []string) {
	store := openStoreOrExit()
	defer store.Close()
	exitOnErr(store.RenameCollection(args[0], args[1]), "Failed to rename collection")
	format.PrintSuccess(fmt.Sprintf("Collection '%s' renamed to '%s'", args[0], args[1]))
}

func runCollectionRemoveRequest(cmd *cobra.Command, args []string) {
	store, col := loadCollectionOrExit(args[0])
	defer store.Close()

	idx, err := findRequestIndex(col, args[1])
	exitOnErr(err, "Failed to remove request")
	exitOnErr(store.RemoveFromCollection(col.Name, idx), "Failed to remove request")
	format.PrintSuccess(fmt.Sprintf("Removed request %d from collection '%s'", idx+1, col.Name))
}

func runCollectionEdit(cmd *cobra.Command, args []string) {
	store, col := loadCollectionOrExit(args[0])
	defer store.Close()

	idx, err := findRequestIndex(col, args[1])
	exitOnErr(err, "Failed to edit request")
	req := col.Requests[idx]

	flags := cmd.Flags()
	if !(flags.Changed("name") || flags.Changed("method") || flags.Changed("url") ||
		flags.Changed("header") || flags.Changed("data") ||
		flags.Changed("assert") || flags.Changed("clear-assert")) {
		format.PrintError("Nothing to change: pass --name, --method, --url, -H, -d, --assert or --clear-assert")
		os.Exit(1)
	}

	if flags.Changed("name") {
		req.Name, _ = flags.GetString("name")
	}
	if flags.Changed("method") {
		m, _ := flags.GetString("method")
		req.Method = strings.ToUpper(strings.TrimSpace(m))
		if req.Method == "" {
			format.PrintError("Method must not be empty")
			os.Exit(1)
		}
	}
	if flags.Changed("url") {
		req.URL, _ = flags.GetString("url")
		if req.URL == "" {
			format.PrintError("URL must not be empty")
			os.Exit(1)
		}
	}
	if flags.Changed("header") {
		merged := make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			merged[k] = v
		}
		for _, h := range headers {
			key, _, ok := strings.Cut(h, ":")
			if !ok || strings.TrimSpace(key) == "" {
				format.PrintError(fmt.Sprintf("Invalid header %q: expected 'Name: value'", h))
				os.Exit(1)
			}
			key = strings.TrimSpace(key)
			// Drop any existing header with the same name (case-insensitive)
			for existing := range merged {
				if strings.EqualFold(existing, key) {
					delete(merged, existing)
				}
			}
			for k, v := range filterSensitiveHeaders(parseHeaders([]string{h})) {
				if v != "" {
					merged[k] = v
				}
			}
		}
		req.Headers = merged
	}
	if flags.Changed("data") {
		req.Body = data
	}
	if flags.Changed("assert") {
		exitOnErr(validateAssertions(assertFlags), "Invalid assertion")
		req.Assertions = assertFlags
	}
	if clear, _ := flags.GetBool("clear-assert"); clear {
		req.Assertions = nil
	}

	exitOnErr(store.UpdateInCollection(col.Name, idx, req), "Failed to edit request")
	format.PrintSuccess(fmt.Sprintf("Updated request %d in collection '%s'", idx+1, col.Name))
}
