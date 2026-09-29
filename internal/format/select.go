package format

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SelectJSON extracts a value from a JSON body using a simple path such as
// ".data.items[0].name". Strings are returned unquoted; other values are
// returned as indented JSON.
func SelectJSON(body, path string) (string, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return "", fmt.Errorf("response body is not valid JSON: %w", err)
	}

	steps, err := parseSelectPath(path)
	if err != nil {
		return "", err
	}

	for _, step := range steps {
		switch cur := v.(type) {
		case map[string]interface{}:
			key, ok := step.(string)
			if !ok {
				return "", fmt.Errorf("cannot index object with [%d]", step.(int))
			}
			next, found := cur[key]
			if !found {
				return "", fmt.Errorf("key %q not found", key)
			}
			v = next
		case []interface{}:
			idx, ok := step.(int)
			if !ok {
				return "", fmt.Errorf("cannot access key %q on an array", step.(string))
			}
			if idx < 0 || idx >= len(cur) {
				return "", fmt.Errorf("index %d out of range (length %d)", idx, len(cur))
			}
			v = cur[idx]
		default:
			return "", fmt.Errorf("cannot descend into a scalar value")
		}
	}

	if s, ok := v.(string); ok {
		return s, nil
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseSelectPath splits ".a.b[0].c" into steps of string keys and int indexes.
func parseSelectPath(path string) ([]interface{}, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return nil, nil
	}

	var steps []interface{}
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end == -1 {
				return nil, fmt.Errorf("invalid path %q: missing ']'", path)
			}
			n, err := strconv.Atoi(path[i+1 : i+end])
			if err != nil {
				return nil, fmt.Errorf("invalid path %q: bad array index %q", path, path[i+1:i+end])
			}
			steps = append(steps, n)
			i += end + 1
		default:
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			steps = append(steps, path[i:j])
			i = j
		}
	}
	return steps, nil
}
