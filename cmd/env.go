package cmd

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"api/internal/format"
	"api/internal/model"
	"api/internal/storage"
)

var (
	envFlag     string
	varOverride []string
	captureSpec []string
)

var varNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// addVariableFlags registers --env, --var and --capture
func addVariableFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&envFlag, "env", "", "Environment to use for {{variables}} (default: the active environment)")
	f.StringArrayVar(&varOverride, "var", nil, "Set a variable key=value for this run (overrides the environment)")
	f.StringArrayVar(&captureSpec, "capture", nil, "Capture a JSON value from the response into a variable: name=.path")
}

// selectedEnvironment returns the environment named by --env or the active one
func selectedEnvironment(store *storage.SQLiteStorage) (string, error) {
	if envFlag != "" {
		exists, err := store.EnvironmentExists(envFlag)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", fmt.Errorf("environment '%s' not found", envFlag)
		}
		return envFlag, nil
	}
	return store.GetActiveEnvironment()
}

// loadVariables returns the variables for this run: the selected environment's
// variables with any --var overrides applied.
func loadVariables() (map[string]string, error) {
	result := map[string]string{}

	store, err := storage.NewStorage()
	if err != nil {
		if envFlag != "" {
			return nil, fmt.Errorf("failed to load environment: %v", err)
		}
	} else {
		defer store.Close()
		name, err := selectedEnvironment(store)
		if err != nil {
			return nil, err
		}
		if name != "" {
			if result, err = store.GetEnvVars(name); err != nil {
				return nil, fmt.Errorf("failed to load environment '%s': %v", name, err)
			}
		}
	}

	for _, o := range varOverride {
		k, v, err := splitKeyValue(o)
		if err != nil {
			return nil, fmt.Errorf("--var: %w", err)
		}
		result[k] = v
	}
	return result, nil
}

// captureFromResponse evaluates the --capture specs against resp and stores
// the values in vars. With persist set (single requests), values are also saved
// to the selected environment and a path that isn't found is an error. Without
// it (collection runs, where each spec is tried against every response), a
// response that lacks the path is skipped.
func captureFromResponse(resp *model.Response, vars map[string]string, persist bool) error {
	if len(captureSpec) == 0 {
		return nil
	}

	var store *storage.SQLiteStorage
	envName := ""
	if persist {
		var err error
		if store, err = storage.NewStorage(); err != nil {
			return fmt.Errorf("--capture: cannot save values: %v", err)
		}
		defer store.Close()
		if envName, err = selectedEnvironment(store); err != nil {
			return fmt.Errorf("--capture: %v", err)
		}
	}

	for _, spec := range captureSpec {
		name, path, err := splitKeyValue(spec)
		if err != nil || !varNamePattern.MatchString(name) {
			return fmt.Errorf("--capture: expected name=.path, got %q", spec)
		}
		value, err := format.SelectJSON(resp.Body, path)
		if err != nil {
			if !persist {
				continue
			}
			return fmt.Errorf("--capture %s: %v", name, err)
		}
		vars[name] = value

		if persist {
			if envName == "" {
				return fmt.Errorf("--capture %s: no environment selected, value not saved (see 'apicli env use')", name)
			}
			if err := store.SetEnvVar(envName, name, value); err != nil {
				return fmt.Errorf("--capture %s: %v", name, err)
			}
			format.PrintSuccess(fmt.Sprintf("Captured %s into environment '%s'", name, envName))
		}
	}
	return nil
}

func init() {
	envCmd := &cobra.Command{
		Use:     "env",
		Aliases: []string{"environment"},
		Short:   "Manage environments and variables",
		Long: `Manage named environments of variables.

Use {{name}} placeholders in URLs, headers, query parameters and bodies; they
are replaced when a request is sent. Stored history and collections keep the
placeholders, so variable values (such as tokens) are never written there.

Example:
  apicli env create dev
  apicli env set dev base=https://api.dev.example.com token=abc123
  apicli env use dev
  apicli get '{{base}}/users' -H 'Authorization: Bearer {{token}}'`,
	}

	listCmd := &cobra.Command{Use: "list", Short: "List environments", Run: runEnvList}
	createCmd := &cobra.Command{Use: "create <name>", Short: "Create an environment", Args: cobra.ExactArgs(1), Run: runEnvCreate}
	showCmd := &cobra.Command{Use: "show [name]", Short: "Show an environment's variables (default: active)", Args: cobra.MaximumNArgs(1), Run: runEnvShow}
	setCmd := &cobra.Command{Use: "set <name> KEY=VALUE...", Short: "Set variables in an environment", Args: cobra.MinimumNArgs(2), Run: runEnvSet}
	unsetCmd := &cobra.Command{Use: "unset <name> KEY...", Short: "Remove variables from an environment", Args: cobra.MinimumNArgs(2), Run: runEnvUnset}
	useCmd := &cobra.Command{Use: "use <name>", Short: "Make an environment active", Args: cobra.MaximumNArgs(1), Run: runEnvUse}
	useCmd.Flags().Bool("clear", false, "Deactivate the current environment")
	deleteCmd := &cobra.Command{Use: "delete <name>", Short: "Delete an environment", Args: cobra.ExactArgs(1), Run: runEnvDelete}

	envCmd.AddCommand(listCmd, createCmd, showCmd, setCmd, unsetCmd, useCmd, deleteCmd)
	rootCmd.AddCommand(envCmd)
}

func openStoreOrExit() *storage.SQLiteStorage {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to open storage: %v", err))
		os.Exit(1)
	}
	return store
}

func exitOnErr(err error, what string) {
	if err != nil {
		format.PrintError(fmt.Sprintf("%s: %v", what, err))
		os.Exit(1)
	}
}

func requireEnv(store *storage.SQLiteStorage, name string) {
	exists, err := store.EnvironmentExists(name)
	exitOnErr(err, "Failed to look up environment")
	if !exists {
		format.PrintError(fmt.Sprintf("Environment '%s' not found", name))
		os.Exit(1)
	}
}

func runEnvList(cmd *cobra.Command, args []string) {
	store := openStoreOrExit()
	defer store.Close()

	names, err := store.ListEnvironments()
	exitOnErr(err, "Failed to list environments")
	if len(names) == 0 {
		fmt.Println("No environments. Create one with: apicli env create <name>")
		return
	}
	active, _ := store.GetActiveEnvironment()
	for _, n := range names {
		if n == active {
			fmt.Printf("* %s\n", n)
		} else {
			fmt.Printf("  %s\n", n)
		}
	}
}

func runEnvCreate(cmd *cobra.Command, args []string) {
	name := args[0]
	if !varNamePattern.MatchString(name) {
		format.PrintError("Environment names may only contain letters, digits, '_', '-' and '.'")
		os.Exit(1)
	}
	store := openStoreOrExit()
	defer store.Close()

	exists, err := store.EnvironmentExists(name)
	exitOnErr(err, "Failed to create environment")
	if exists {
		format.PrintError(fmt.Sprintf("Environment '%s' already exists", name))
		os.Exit(1)
	}
	exitOnErr(store.CreateEnvironment(name), "Failed to create environment")
	format.PrintSuccess(fmt.Sprintf("Environment '%s' created", name))
}

func runEnvShow(cmd *cobra.Command, args []string) {
	store := openStoreOrExit()
	defer store.Close()

	name := ""
	if len(args) > 0 {
		name = args[0]
	} else {
		var err error
		name, err = store.GetActiveEnvironment()
		exitOnErr(err, "Failed to read active environment")
		if name == "" {
			format.PrintError("No active environment. Specify a name or run 'apicli env use <name>'")
			os.Exit(1)
		}
	}
	requireEnv(store, name)

	values, err := store.GetEnvVars(name)
	exitOnErr(err, "Failed to load environment")
	fmt.Printf("Environment '%s' (%d variable(s))\n", name, len(values))
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s = %s\n", k, values[k])
	}
}

func runEnvSet(cmd *cobra.Command, args []string) {
	name := args[0]
	pairs := make([][2]string, 0, len(args)-1)
	for _, a := range args[1:] {
		k, v, err := splitKeyValue(a)
		if err != nil || !varNamePattern.MatchString(k) {
			format.PrintError(fmt.Sprintf("Invalid variable %q: expected KEY=VALUE with KEY made of letters, digits, '_', '-' or '.'", a))
			os.Exit(1)
		}
		pairs = append(pairs, [2]string{k, v})
	}

	store := openStoreOrExit()
	defer store.Close()
	requireEnv(store, name)
	for _, p := range pairs {
		exitOnErr(store.SetEnvVar(name, p[0], p[1]), "Failed to set variable")
	}
	format.PrintSuccess(fmt.Sprintf("Set %d variable(s) in environment '%s'", len(pairs), name))
}

func runEnvUnset(cmd *cobra.Command, args []string) {
	name := args[0]
	store := openStoreOrExit()
	defer store.Close()
	requireEnv(store, name)

	for _, k := range args[1:] {
		removed, err := store.UnsetEnvVar(name, k)
		exitOnErr(err, "Failed to remove variable")
		if !removed {
			fmt.Fprintf(os.Stderr, "Variable '%s' not set in '%s'\n", k, name)
		}
	}
	format.PrintSuccess(fmt.Sprintf("Updated environment '%s'", name))
}

func runEnvUse(cmd *cobra.Command, args []string) {
	clear, _ := cmd.Flags().GetBool("clear")
	if clear == (len(args) > 0) {
		format.PrintError("Provide an environment name, or use --clear to deactivate")
		os.Exit(1)
	}

	store := openStoreOrExit()
	defer store.Close()
	if clear {
		exitOnErr(store.SetActiveEnvironment(""), "Failed to clear active environment")
		format.PrintSuccess("No environment is active")
		return
	}
	exitOnErr(store.SetActiveEnvironment(args[0]), "Failed to activate environment")
	format.PrintSuccess(fmt.Sprintf("Using environment '%s'", args[0]))
}

func runEnvDelete(cmd *cobra.Command, args []string) {
	name := strings.TrimSpace(args[0])
	store := openStoreOrExit()
	defer store.Close()
	requireEnv(store, name)
	exitOnErr(store.DeleteEnvironment(name), "Failed to delete environment")
	format.PrintSuccess(fmt.Sprintf("Environment '%s' deleted", name))
}
