// Package curl converts between curl command lines and requests.
package curl

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

// Request is the subset of a curl invocation that apicli understands.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string

	// Warnings lists options that were ignored or could not be converted.
	Warnings []string
}

// Tokenize splits a command line into words the way a POSIX shell would:
// whitespace separates words; single quotes are literal; double quotes allow
// backslash escapes of \ " $ ` and newline; a backslash outside quotes
// escapes the next character (backslash-newline is a line continuation).
func Tokenize(s string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	inWord := false
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\'':
			inWord = true
			i++
			for i < len(runes) && runes[i] != '\'' {
				cur.WriteRune(runes[i])
				i++
			}
			if i >= len(runes) {
				return nil, fmt.Errorf("unterminated single quote")
			}
		case r == '"':
			inWord = true
			i++
			for i < len(runes) && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < len(runes) && strings.ContainsRune("\\\"$`\n", runes[i+1]) {
					i++
					if runes[i] == '\n' {
						i++
						continue
					}
				}
				cur.WriteRune(runes[i])
				i++
			}
			if i >= len(runes) {
				return nil, fmt.Errorf("unterminated double quote")
			}
		case r == '\\':
			if i+1 >= len(runes) {
				cur.WriteRune(r)
				inWord = true
				break
			}
			i++
			if runes[i] == '\n' || (runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n') {
				if runes[i] == '\r' {
					i++
				}
				continue
			}
			cur.WriteRune(runes[i])
			inWord = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				tokens = append(tokens, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		tokens = append(tokens, cur.String())
	}
	return tokens, nil
}

// optionsWithValue maps options that take a value to a canonical name.
var optionsWithValue = map[string]string{
	"-X": "request", "--request": "request",
	"-H": "header", "--header": "header",
	"-d": "data", "--data": "data", "--data-raw": "data", "--data-binary": "data", "--data-ascii": "data",
	"--data-urlencode": "data-urlencode",
	"--json":           "json",
	"-u":               "user", "--user": "user",
	"-A": "user-agent", "--user-agent": "user-agent",
	"-b": "cookie", "--cookie": "cookie",
	"-e": "referer", "--referer": "referer",
	"--url": "url",
	// accepted and ignored
	"-o": "ignore", "--output": "ignore", "-m": "ignore", "--max-time": "ignore",
	"--connect-timeout": "ignore", "-x": "ignore", "--proxy": "ignore", "-w": "ignore",
	"--write-out": "ignore", "--retry": "ignore", "-c": "ignore", "--cookie-jar": "ignore",
	"--cacert": "ignore", "--cert": "ignore", "--key": "ignore", "-K": "ignore",
}

// unsupportedWithValue are options whose meaning we cannot reproduce.
var unsupportedWithValue = map[string]bool{
	"-F": true, "--form": true, "--form-string": true, "-T": true, "--upload-file": true,
}

// Parse converts curl arguments (with or without the leading "curl") to a Request.
func Parse(args []string) (*Request, error) {
	if len(args) > 0 && args[0] == "curl" {
		args = args[1:]
	}

	req := &Request{Headers: map[string]string{}}
	var (
		dataParts   []string
		explicit    string
		head        bool
		getMode     bool
		userInfo    string
		cookie      string
		userAgent   string
		referer     string
		jsonBody    bool
		positionals []string
		optsDone    bool
	)

	warn := func(format string, a ...interface{}) {
		req.Warnings = append(req.Warnings, fmt.Sprintf(format, a...))
	}

	apply := func(kind, value, opt string) error {
		switch kind {
		case "request":
			explicit = strings.ToUpper(value)
		case "header":
			name, val, ok := strings.Cut(value, ":")
			name = strings.TrimSpace(name)
			if !ok || name == "" {
				if strings.HasSuffix(value, ";") { // "Name;" sends an empty header
					req.Headers[strings.TrimSuffix(value, ";")] = ""
					return nil
				}
				return fmt.Errorf("invalid header %q", value)
			}
			req.Headers[name] = strings.TrimSpace(val)
		case "data":
			if strings.HasPrefix(value, "@") && opt != "--data-raw" {
				warn("%s %s reads the body from a file; body ignored (use -d @file with apicli instead)", opt, value)
				return nil
			}
			dataParts = append(dataParts, value)
		case "data-urlencode":
			warn("%s is not supported; body ignored", opt)
		case "json":
			jsonBody = true
			dataParts = append(dataParts, value)
		case "user":
			userInfo = value
		case "user-agent":
			userAgent = value
		case "cookie":
			cookie = value
		case "referer":
			referer = value
		case "url":
			positionals = append(positionals, value)
		case "ignore":
			warn("ignored option %s", opt)
		}
		return nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if optsDone || arg == "-" || !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		if arg == "--" {
			optsDone = true
			continue
		}

		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch {
			case name == "--head":
				head = true
			case name == "--get":
				getMode = true
			case optionsWithValue[name] != "" || unsupportedWithValue[name]:
				if !hasValue {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("option %s needs a value", name)
					}
					i++
					value = args[i]
				}
				if unsupportedWithValue[name] {
					warn("%s is not supported; ignored", name)
					continue
				}
				if err := apply(optionsWithValue[name], value, name); err != nil {
					return nil, err
				}
			default:
				// Unknown long flags (--compressed, --location, --silent ...) are
				// assumed to take no value and are ignored.
			}
			continue
		}

		// Short options, possibly combined (-sSL) or with an attached value (-XPOST)
		for j := 1; j < len(arg); j++ {
			opt := "-" + string(arg[j])
			switch {
			case opt == "-I":
				head = true
			case opt == "-G":
				getMode = true
			case optionsWithValue[opt] != "" || unsupportedWithValue[opt]:
				value := arg[j+1:]
				if value == "" {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("option %s needs a value", opt)
					}
					i++
					value = args[i]
				}
				if unsupportedWithValue[opt] {
					warn("%s is not supported; ignored", opt)
				} else if err := apply(optionsWithValue[opt], value, opt); err != nil {
					return nil, err
				}
				j = len(arg) // the value consumed the rest of the token
			}
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("no URL found in curl command")
	}
	if len(positionals) > 1 {
		warn("multiple URLs given; using the first (%s)", positionals[0])
	}
	req.URL = positionals[0]
	if !strings.Contains(req.URL, "://") {
		req.URL = "http://" + req.URL
	}

	if userInfo != "" {
		req.Headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(userInfo))
	}
	if userAgent != "" {
		req.Headers["User-Agent"] = userAgent
	}
	if cookie != "" {
		req.Headers["Cookie"] = cookie
	}
	if referer != "" {
		req.Headers["Referer"] = referer
	}
	if jsonBody {
		setDefaultHeader(req.Headers, "Content-Type", "application/json")
		setDefaultHeader(req.Headers, "Accept", "application/json")
	}

	body := strings.Join(dataParts, "&")
	if jsonBody {
		body = strings.Join(dataParts, "")
	}
	if getMode && body != "" {
		sep := "?"
		if strings.Contains(req.URL, "?") {
			sep = "&"
		}
		req.URL += sep + body
		body = ""
	}
	req.Body = body

	switch {
	case explicit != "":
		req.Method = explicit
	case head:
		req.Method = "HEAD"
	case req.Body != "":
		req.Method = "POST"
	default:
		req.Method = "GET"
	}
	if req.Body != "" && !hasHeader(req.Headers, "Content-Type") && !jsonBody {
		// curl -d defaults to a form content type; apicli would default to JSON
		req.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return req, nil
}

func hasHeader(h map[string]string, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func setDefaultHeader(h map[string]string, name, value string) {
	if !hasHeader(h, name) {
		h[name] = value
	}
}

// quote wraps s in single quotes for a POSIX shell.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Build renders a request as a curl command line. Headers are sorted so the
// output is stable.
func Build(method, url string, headers map[string]string, body string) string {
	var b strings.Builder
	b.WriteString("curl")

	switch method {
	case "", "GET":
	case "HEAD":
		b.WriteString(" --head")
	default:
		b.WriteString(" -X " + method)
	}
	b.WriteString(" " + quote(url))

	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(" -H " + quote(k+": "+headers[k]))
	}
	if body != "" {
		b.WriteString(" -d " + quote(body))
	}
	return b.String()
}
