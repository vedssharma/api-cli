package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "apicli",
	Short: "A CLI tool for making HTTP requests",
	Version: version,
	Long: `apicli is a command-line HTTP client, similar to Postman.

Send HTTP requests, track history, and organize requests into collections.

Examples:
  apicli get https://api.example.com/users
  apicli post https://api.example.com/users -d '{"name": "John"}'
  apicli history
  apicli collection list`,
}

// version is overridden at build time with -ldflags "-X api/cmd.version=..."
var version = "dev"

// Execute runs the root command
func Execute() {
	registerDynamicCompletions()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Global flags can be added here
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Show response headers")
}
