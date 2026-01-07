package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/vedsharma/apicli/internal/format"
	"github.com/vedsharma/apicli/internal/storage"
)

func init() {
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "View request history",
		Run:   runHistoryList,
	}

	historyCmd.Flags().IntP("limit", "n", 10, "Number of requests to show")

	showCmd := &cobra.Command{
		Use:   "show <id or index>",
		Short: "Show full details of a request",
		Args:  cobra.ExactArgs(1),
		Run:   runHistoryShow,
	}

	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear all history",
		Run:   runHistoryClear,
	}

	historyCmd.AddCommand(showCmd, clearCmd)
	rootCmd.AddCommand(historyCmd)
}

func runHistoryList(cmd *cobra.Command, args []string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load history: %v", err))
		os.Exit(1)
	}

	history, err := store.LoadHistory()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load history: %v", err))
		os.Exit(1)
	}

	limit, _ := cmd.Flags().GetInt("limit")
	format.PrintHistoryList(history.Requests, limit)
}

func runHistoryShow(cmd *cobra.Command, args []string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load history: %v", err))
		os.Exit(1)
	}

	history, err := store.LoadHistory()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to load history: %v", err))
		os.Exit(1)
	}

	identifier := args[0]

	// Try to parse as index first (1-based)
	if index, err := strconv.Atoi(identifier); err == nil {
		if index > 0 && index <= len(history.Requests) {
			format.PrintRequestDetail(&history.Requests[index-1])
			return
		}
	}

	// Try to find by ID
	for _, req := range history.Requests {
		if req.ID == identifier {
			format.PrintRequestDetail(&req)
			return
		}
	}

	format.PrintError(fmt.Sprintf("Request not found: %s", identifier))
	os.Exit(1)
}

func runHistoryClear(cmd *cobra.Command, args []string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to clear history: %v", err))
		os.Exit(1)
	}

	if err := store.ClearHistory(); err != nil {
		format.PrintError(fmt.Sprintf("Failed to clear history: %v", err))
		os.Exit(1)
	}

	format.PrintSuccess("History cleared")
}
