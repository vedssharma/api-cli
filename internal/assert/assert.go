// Package assert evaluates simple assertions against an HTTP response.
//
// An assertion is written as <target><operator><value>:
//
//	status=200          status code
//	body~"ok"           response body (contains)
//	header:Content-Type~json
//	.data.id=5          value at a JSON path
//	.data.id            the JSON path exists
//
// Operators: = != ~ (contains) !~ (does not contain) < > <= >= (numeric).
package assert

import (
	"fmt"
	"net/textproto"
	"strconv"
	"strings"

	"api/internal/format"
	"api/internal/model"
)

// operators, longest first so "!=" wins over "=".
var operators = []string{"!=", "!~", "<=", ">=", "=", "~", "<", ">"}

// Assertion is a parsed assertion.
type Assertion struct {
	Raw    string
	target string // "status", "body", "header:Name" or ".json.path"
	op     string // empty means an existence check (JSON paths only)
	value  string
}

// Parse parses an assertion expression.
func Parse(expr string) (*Assertion, error) {
	raw := strings.TrimSpace(expr)
	idx := strings.IndexAny(raw, "=!~<>")

	target, op, value := raw, "", ""
	if idx >= 0 {
		target = strings.TrimSpace(raw[:idx])
		for _, candidate := range operators {
			if strings.HasPrefix(raw[idx:], candidate) {
				op = candidate
				break
			}
		}
		if op == "" {
			return nil, fmt.Errorf("invalid assertion %q: unknown operator", expr)
		}
		value = strings.TrimSpace(raw[idx+len(op):])
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
	}

	switch {
	case target == "status":
		if op == "" || op == "~" || op == "!~" {
			return nil, fmt.Errorf("invalid assertion %q: status supports = != < > <= >=", expr)
		}
		if _, err := strconv.Atoi(value); err != nil {
			return nil, fmt.Errorf("invalid assertion %q: status must be a number", expr)
		}
	case target == "body":
		if op == "" {
			return nil, fmt.Errorf("invalid assertion %q: body needs an operator, e.g. body~text", expr)
		}
	case strings.HasPrefix(target, "header:"):
		if strings.TrimSpace(strings.TrimPrefix(target, "header:")) == "" || op == "" {
			return nil, fmt.Errorf("invalid assertion %q: expected header:Name<op>value", expr)
		}
	case strings.HasPrefix(target, "."):
	default:
		return nil, fmt.Errorf("invalid assertion %q: target must be status, body, header:Name or a JSON path like .a.b", expr)
	}

	if (op == "<" || op == ">" || op == "<=" || op == ">=") && target != "status" {
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return nil, fmt.Errorf("invalid assertion %q: %s needs a numeric value", expr, op)
		}
	}
	return &Assertion{Raw: raw, target: target, op: op, value: value}, nil
}

// Check evaluates the assertion. detail describes why it failed (empty on success).
func (a *Assertion) Check(resp *model.Response) (ok bool, detail string) {
	var actual string
	switch {
	case a.target == "status":
		actual = strconv.Itoa(resp.StatusCode)
	case a.target == "body":
		actual = resp.Body
	case strings.HasPrefix(a.target, "header:"):
		name := strings.TrimSpace(strings.TrimPrefix(a.target, "header:"))
		v, found := lookupHeader(resp.Headers, name)
		if !found {
			return a.op == "!=" || a.op == "!~", fmt.Sprintf("header %s not present", name)
		}
		actual = v
	default:
		v, err := format.SelectJSON(resp.Body, a.target)
		if err != nil {
			return false, err.Error()
		}
		if a.op == "" {
			return true, ""
		}
		actual = v
	}

	pass, err := compare(actual, a.op, a.value)
	if err != nil {
		return false, err.Error()
	}
	if pass {
		return true, ""
	}
	if a.target == "body" {
		return false, "body did not match"
	}
	return false, fmt.Sprintf("got %s", actual)
}

func lookupHeader(headers map[string]string, name string) (string, bool) {
	canonical := textproto.CanonicalMIMEHeaderKey(name)
	for k, v := range headers {
		if textproto.CanonicalMIMEHeaderKey(k) == canonical {
			return v, true
		}
	}
	return "", false
}

func compare(actual, op, expected string) (bool, error) {
	switch op {
	case "=":
		return actual == expected, nil
	case "!=":
		return actual != expected, nil
	case "~":
		return strings.Contains(actual, expected), nil
	case "!~":
		return !strings.Contains(actual, expected), nil
	}

	a, err := strconv.ParseFloat(strings.TrimSpace(actual), 64)
	if err != nil {
		return false, fmt.Errorf("got %q, which is not a number", actual)
	}
	e, _ := strconv.ParseFloat(expected, 64)
	switch op {
	case "<":
		return a < e, nil
	case ">":
		return a > e, nil
	case "<=":
		return a <= e, nil
	case ">=":
		return a >= e, nil
	}
	return false, fmt.Errorf("unknown operator %q", op)
}
