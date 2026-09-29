package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"api/internal/storage"
)

// registerDynamicCompletions teaches the shell completion scripts (generated
// by `apicli completion <shell>`) to suggest collection, environment, alias
// and history names from local storage.
func registerDynamicCompletions() {
	firstArg := func(fn func(prefix string) []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return fn(toComplete), cobra.ShellCompDirectiveNoFileComp
		}
	}
	flagValues := func(fn func(prefix string) []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return fn(toComplete), cobra.ShellCompDirectiveNoFileComp
		}
	}

	setArgs := func(path string, fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
		if c, _, err := rootCmd.Find(strings.Fields(path)); err == nil && c != rootCmd {
			c.ValidArgsFunction = fn
		}
	}
	for _, p := range []string{
		"collection show", "collection delete", "collection run", "collection rename",
		"collection add", "collection remove-request", "collection edit",
	} {
		setArgs(p, firstArg(completeCollections))
	}
	for _, p := range []string{"env show", "env set", "env unset", "env use", "env delete"} {
		setArgs(p, firstArg(completeEnvironments))
	}
	for _, p := range []string{"alias show", "alias delete"} {
		setArgs(p, firstArg(completeAliases))
	}
	for _, p := range []string{"history show", "history replay"} {
		setArgs(p, firstArg(completeHistory))
	}

	// Flags that take a collection or environment name
	register := func(path, flag string, fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
		c, _, err := rootCmd.Find(strings.Fields(path))
		if err != nil || c == rootCmd || c.Flags().Lookup(flag) == nil {
			return
		}
		_ = c.RegisterFlagCompletionFunc(flag, fn)
	}
	requestCmds := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	for _, p := range requestCmds {
		register(p, "collection", flagValues(completeCollections))
		register(p, "env", flagValues(completeEnvironments))
	}
	for _, p := range []string{"collection run", "history replay"} {
		register(p, "env", flagValues(completeEnvironments))
	}
	for _, p := range []string{"import postman", "import curl", "import har", "import openapi", "export postman", "export curl"} {
		register(p, "collection", flagValues(completeCollections))
	}
}

func withPrefix(names []string, prefix string) []string {
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

func completeCollections(prefix string) []string {
	store, err := storage.NewStorage()
	if err != nil {
		return nil
	}
	defer store.Close()
	all, err := store.LoadCollections()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(all.Collections))
	for n := range all.Collections {
		names = append(names, n)
	}
	sort.Strings(names)
	return withPrefix(names, prefix)
}

func completeEnvironments(prefix string) []string {
	store, err := storage.NewStorage()
	if err != nil {
		return nil
	}
	defer store.Close()
	names, err := store.ListEnvironments()
	if err != nil {
		return nil
	}
	return withPrefix(names, prefix)
}

func completeAliases(prefix string) []string {
	store, err := storage.NewStorage()
	if err != nil {
		return nil
	}
	defer store.Close()
	aliases, err := store.LoadAliases()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(aliases.Aliases))
	for n := range aliases.Aliases {
		names = append(names, n)
	}
	sort.Strings(names)
	return withPrefix(names, prefix)
}

// completeHistory suggests request IDs with "METHOD URL" descriptions
func completeHistory(prefix string) []string {
	store, err := storage.NewStorage()
	if err != nil {
		return nil
	}
	defer store.Close()
	h, err := store.LoadHistory()
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range h.Requests {
		if strings.HasPrefix(r.ID, prefix) {
			out = append(out, fmt.Sprintf("%s\t%s %s", r.ID, r.Method, r.URL))
		}
	}
	return out
}
