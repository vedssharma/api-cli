package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/vedsharma/apicli/internal/format"
	httpclient "github.com/vedsharma/apicli/internal/http"
	"github.com/vedsharma/apicli/internal/model"
	"github.com/vedsharma/apicli/internal/storage"
)

var (
	headers     []string
	data        string
	noHistory   bool
	saveToCollection string
)

func init() {
	// GET command
	getCmd := &cobra.Command{
		Use:   "get <url>",
		Short: "Send a GET request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("GET"),
	}
	addRequestFlags(getCmd)
	rootCmd.AddCommand(getCmd)

	// POST command
	postCmd := &cobra.Command{
		Use:   "post <url>",
		Short: "Send a POST request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("POST"),
	}
	addRequestFlags(postCmd)
	rootCmd.AddCommand(postCmd)

	// PUT command
	putCmd := &cobra.Command{
		Use:   "put <url>",
		Short: "Send a PUT request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("PUT"),
	}
	addRequestFlags(putCmd)
	rootCmd.AddCommand(putCmd)

	// PATCH command
	patchCmd := &cobra.Command{
		Use:   "patch <url>",
		Short: "Send a PATCH request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("PATCH"),
	}
	addRequestFlags(patchCmd)
	rootCmd.AddCommand(patchCmd)

	// DELETE command
	deleteCmd := &cobra.Command{
		Use:   "delete <url>",
		Short: "Send a DELETE request",
		Args:  cobra.ExactArgs(1),
		Run:   runRequest("DELETE"),
	}
	addRequestFlags(deleteCmd)
	rootCmd.AddCommand(deleteCmd)
}

func addRequestFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(&headers, "header", "H", []string{}, "Add header (can be used multiple times)")
	cmd.Flags().StringVarP(&data, "data", "d", "", "Request body (JSON string or @filename)")
	cmd.Flags().BoolVar(&noHistory, "no-history", false, "Don't save to history")
	cmd.Flags().StringVarP(&saveToCollection, "collection", "c", "", "Save to collection")
}

func runRequest(method string) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		url := args[0]
		verbose, _ := cmd.Flags().GetBool("verbose")

		// Parse headers
		headerMap := parseHeaders(headers)

		// Read body from file if prefixed with @
		body := data
		if strings.HasPrefix(body, "@") {
			filename := strings.TrimPrefix(body, "@")
			content, err := os.ReadFile(filename)
			if err != nil {
				format.PrintError(fmt.Sprintf("Failed to read file: %v", err))
				os.Exit(1)
			}
			body = string(content)
		}

		// Create HTTP client and make request
		client := httpclient.NewClient()
		resp, err := client.Do(method, url, headerMap, body)
		if err != nil {
			format.PrintError(fmt.Sprintf("Request failed: %v", err))
			os.Exit(1)
		}

		// Print response
		format.PrintResponse(resp, verbose)

		// Save to history unless disabled
		if !noHistory {
			saveToHistory(method, url, headerMap, body, resp)
		}

		// Save to collection if specified
		if saveToCollection != "" {
			saveRequestToCollection(saveToCollection, method, url, headerMap, body)
		}
	}
}

func parseHeaders(headerStrings []string) map[string]string {
	result := make(map[string]string)
	for _, h := range headerStrings {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result[key] = value
		}
	}
	return result
}

func saveToHistory(method, url string, headers map[string]string, body string, resp *model.Response) {
	store, err := storage.NewStorage()
	if err != nil {
		// Silently fail - don't interrupt the user
		return
	}

	req := model.Request{
		ID:        uuid.New().String()[:8],
		Timestamp: time.Now(),
		Method:    method,
		URL:       url,
		Headers:   headers,
		Body:      body,
		Response:  resp,
	}

	_ = store.AddToHistory(req)
}

func saveRequestToCollection(collectionName, method, url string, headers map[string]string, body string) {
	store, err := storage.NewStorage()
	if err != nil {
		format.PrintError(fmt.Sprintf("Failed to save to collection: %v", err))
		return
	}

	req := model.SavedRequest{
		Name:    "",
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    body,
	}

	if err := store.AddToCollection(collectionName, req); err != nil {
		format.PrintError(fmt.Sprintf("Failed to save to collection: %v", err))
		return
	}

	format.PrintSuccess(fmt.Sprintf("Saved to collection '%s'", collectionName))
}
