package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"api/internal/format"
	"api/internal/storage"
)

func init() {
	aliasCmd := &cobra.Command{
		Use:     "alias",
		Aliases: []string{"a"},
		Short:   "Manage URL aliases",
		Long: `Manage URL aliases for frequently used endpoints.

Aliases allow you to create shortcuts for base URLs, so you can use
'starwars/people/1' instead of 'https://www.swapi.tech/api/people/1'.`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all aliases",
		Run:   runAliasList,
	}

	createCmd := &cobra.Command{
		Use:   "create <name> <url>",
		Short: "Create a new alias",
		Long: `Create a new alias for a base URL.

Example:
  apicli alias create starwars https://www.swapi.tech/api
  apicli get starwars/people/1`,
		Args: cobra.ExactArgs(2),
		Run:  runAliasCreate,
	}

	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show an alias",
		Args:  cobra.ExactArgs(1),
		Run:   runAliasShow,
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an alias",
		Args:  cobra.ExactArgs(1),
		Run:   runAliasDelete,
	}

	aliasCmd.AddCommand(listCmd, createCmd, showCmd, deleteCmd)
	rootCmd.AddCommand(aliasCmd)
}

func runAliasList(cmd *cobra.Command, args []string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load aliases: %v", err))
		os.Exit(1)
	}

	aliases, err := store.LoadAliases()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load aliases: %v", err))
		os.Exit(1)
	}

	format.PrintAliasList(aliases)
}

func runAliasCreate(cmd *cobra.Command, args []string) {
	name := args[0]
	url := args[1]

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to create alias: %v", err))
		os.Exit(1)
	}

	if err := store.CreateAlias(name, url); err != nil {
		format.PrintError(fmt.Sprintf("Failed to create alias: %v", err))
		os.Exit(1)
	}

	format.PrintSuccess(fmt.Sprintf("Alias '%s' created for %s", name, url))
}

func runAliasShow(cmd *cobra.Command, args []string) {
	name := args[0]

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load alias: %v", err))
		os.Exit(1)
	}

	url, exists, err := store.GetAlias(name)
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load alias: %v", err))
		os.Exit(1)
	}

	if !exists {
		format.PrintError(fmt.Sprintf("Alias '%s' not found", name))
		os.Exit(1)
	}

	format.PrintAlias(name, url)
}

func runAliasDelete(cmd *cobra.Command, args []string) {
	name := args[0]

	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to delete alias: %v", err))
		os.Exit(1)
	}

	if err := store.DeleteAlias(name); err != nil {
		format.PrintError(fmt.Sprintf("Failed to delete alias: %v", err))
		os.Exit(1)
	}

	format.PrintSuccess(fmt.Sprintf("Alias '%s' deleted", name))
}
