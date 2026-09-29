package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"api/internal/format"
	"api/internal/model"
	"api/internal/storage"
	"api/internal/vars"
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

	searchCmd := &cobra.Command{
		Use:   "search [term]",
		Short: "Search request history",
		Long: `Search request history by URL or method, optionally filtered by response status.
Indexes shown are the same ones 'history show' and 'history replay' accept.

Examples:
  apicli history search users
  apicli history search --status 4xx
  apicli history search --method POST --status 201`,
		Args: cobra.MaximumNArgs(1),
		Run:  runHistorySearch,
	}
	searchCmd.Flags().String("method", "", "Only requests with this HTTP method")
	searchCmd.Flags().String("status", "", "Only responses with this status: 404, or a class like 4xx")
	searchCmd.Flags().IntP("limit", "n", 20, "Maximum number of matches to show")

	replayCmd := &cobra.Command{
		Use:   "replay <id or index>",
		Short: "Send a request from history again",
		Long: `Send a request from history again. {{variables}} are filled in from the
environment. Sensitive headers are not stored in history, so they are dropped
on replay: supply them again with -H, --bearer or --basic, or store them as a
{{variable}} in the original request.`,
		Args: cobra.ExactArgs(1),
		Run:  runHistoryReplay,
	}
	replayCmd.Flags().StringArrayVarP(&headers, "header", "H", []string{}, "Add or replace a header")
	replayCmd.Flags().StringVar(&bearerToken, "bearer", "", "Set Authorization: Bearer <token>")
	replayCmd.Flags().StringVar(&basicAuth, "basic", "", "Set HTTP basic auth as user:password")
	replayCmd.Flags().BoolVar(&noHistory, "no-history", false, "Don't save the replay to history")
	addConnectionFlags(replayCmd)
	addVariableFlags(replayCmd)
	addOutputFlags(replayCmd)

	historyCmd.AddCommand(showCmd, clearCmd, searchCmd, replayCmd)
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

// findHistoryRequest looks up a request by 1-based index or by ID. It returns
// the request and its 1-based index.
func findHistoryRequest(history *model.History, identifier string) (*model.Request, int, bool) {
	if index, err := strconv.Atoi(identifier); err == nil {
		if index > 0 && index <= len(history.Requests) {
			return &history.Requests[index-1], index, true
		}
	}
	for i := range history.Requests {
		if history.Requests[i].ID == identifier {
			return &history.Requests[i], i + 1, true
		}
	}
	return nil, 0, false
}

func loadHistoryOrExit() *model.History {
	store := openStoreOrExit()
	defer store.Close()
	history, err := store.LoadHistory()
	exitOnErr(err, "Failed to load history")
	return history
}

func runHistoryShow(cmd *cobra.Command, args []string) {
	history := loadHistoryOrExit()
	req, _, ok := findHistoryRequest(history, args[0])
	if !ok {
		format.PrintError(fmt.Sprintf("Request not found: %s", args[0]))
		os.Exit(1)
	}
	format.PrintRequestDetail(req)
}

// statusMatches reports whether code matches a filter such as "404" or "4xx"
func statusMatches(filter string, code int) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if len(filter) == 3 && strings.HasSuffix(filter, "xx") {
		return filter[0] >= '1' && filter[0] <= '5' && int(filter[0]-'0') == code/100
	}
	n, err := strconv.Atoi(filter)
	return err == nil && n == code
}

// validStatusFilter reports whether filter is a status code or class like "4xx"
func validStatusFilter(filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if len(filter) == 3 && strings.HasSuffix(filter, "xx") {
		return filter[0] >= '1' && filter[0] <= '5'
	}
	n, err := strconv.Atoi(filter)
	return err == nil && n >= 100 && n <= 599
}

// searchHistory returns the 0-based indexes of requests matching the filters
func searchHistory(requests []model.Request, term, method, status string) []int {
	term = strings.ToLower(term)
	var matches []int
	for i, r := range requests {
		if term != "" && !strings.Contains(strings.ToLower(r.URL), term) &&
			!strings.Contains(strings.ToLower(r.Method), term) {
			continue
		}
		if method != "" && !strings.EqualFold(r.Method, method) {
			continue
		}
		if status != "" && (r.Response == nil || !statusMatches(status, r.Response.StatusCode)) {
			continue
		}
		matches = append(matches, i)
	}
	return matches
}

func runHistorySearch(cmd *cobra.Command, args []string) {
	term := ""
	if len(args) > 0 {
		term = args[0]
	}
	method, _ := cmd.Flags().GetString("method")
	status, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")

	if term == "" && method == "" && status == "" {
		format.PrintError("Provide a search term, --method or --status")
		os.Exit(1)
	}
	if status != "" && !validStatusFilter(status) {
		format.PrintError(fmt.Sprintf("Invalid --status %q: use a code like 404 or a class like 4xx", status))
		os.Exit(1)
	}

	history := loadHistoryOrExit()
	matches := searchHistory(history.Requests, term, method, status)
	if len(matches) == 0 {
		fmt.Println("No matching requests")
		return
	}
	format.PrintHistoryMatches(history.Requests, matches, limit)
}

// replayRequest builds a history entry into a request, passing every value
// through resolve. Redacted headers are dropped (they hold no usable value);
// -H, --bearer and --basic can supply them again.
func replayRequest(req *model.Request, resolve func(string) string) (*builtRequest, []string, error) {
	hdrs := make(map[string]string, len(req.Headers))
	var dropped []string
	for k, v := range req.Headers {
		if v == redactedValue {
			dropped = append(dropped, k)
			continue
		}
		hdrs[resolve(k)] = resolve(v)
	}
	for k, v := range parseHeaders(resolveAll(headers, resolve)) {
		for existing := range hdrs {
			if strings.EqualFold(existing, k) {
				delete(hdrs, existing)
			}
		}
		hdrs[k] = v
	}

	body, err := applyAuthAndBody(hdrs, resolve(req.Body), resolve)
	if err != nil {
		return nil, nil, err
	}
	var missing []string
	for _, k := range dropped {
		if !hasHeader(hdrs, k) {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return &builtRequest{url: resolve(req.URL), headers: hdrs, body: body}, missing, nil
}

func runHistoryReplay(cmd *cobra.Command, args []string) {
	verbose, _ := cmd.Flags().GetBool("verbose")
	history := loadHistoryOrExit()
	req, _, ok := findHistoryRequest(history, args[0])
	if !ok {
		format.PrintError(fmt.Sprintf("Request not found: %s", args[0]))
		os.Exit(1)
	}

	stored, missing, err := replayRequest(req, identity)
	exitOnErr(err, "Failed to replay request")

	varMap, err := loadVariables()
	exitOnErr(err, "Failed to replay request")
	resolver := vars.NewResolver(varMap)
	sent, _, err := replayRequest(req, resolver.String)
	if err == nil {
		err = resolver.Err()
	}
	exitOnErr(err, "Failed to replay request")

	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: not sent (redacted in history): %s. Use -H, --bearer or --basic to supply them.\n",
			strings.Join(missing, ", "))
	}

	fmt.Fprintf(os.Stderr, "Replaying %s %s\n", req.Method, stored.url)
	sendAndReport(req.Method, stored, sent, varMap, verbose)
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
