// Package openapi converts OpenAPI 3.x and Swagger 2.0 documents (JSON or
// YAML) into saved requests.
package openapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"api/internal/model"
)

// Options tunes the conversion.
type Options struct {
	// BaseURL overrides the server URL found in the document. It may be a
	// {{variable}} such as {{baseUrl}}.
	BaseURL string
}

// Result is the outcome of a conversion.
type Result struct {
	Title    string
	BaseURL  string // base URL used in the requests
	Requests []model.SavedRequest
	Warnings []string
}

type obj = map[string]interface{}

var pathTemplate = regexp.MustCompile(`\{([^{}/]+)\}`)

var methodOrder = []string{"get", "post", "put", "patch", "delete", "head", "options"}

// Convert parses an OpenAPI document and returns one request per operation.
func Convert(data []byte, opts Options) (*Result, error) {
	doc, err := parseDocument(data)
	if err != nil {
		return nil, err
	}

	c := &converter{doc: doc, res: &Result{}}
	switch {
	case str(doc["openapi"]) != "":
		if !strings.HasPrefix(str(doc["openapi"]), "3.") {
			return nil, fmt.Errorf("unsupported OpenAPI version %q", str(doc["openapi"]))
		}
	case str(doc["swagger"]) != "":
		if !strings.HasPrefix(str(doc["swagger"]), "2.") {
			return nil, fmt.Errorf("unsupported Swagger version %q", str(doc["swagger"]))
		}
		c.v2 = true
	default:
		return nil, fmt.Errorf("not an OpenAPI document: missing \"openapi\" or \"swagger\" field")
	}

	if info, ok := doc["info"].(obj); ok {
		c.res.Title = str(info["title"])
	}
	c.baseURL = opts.BaseURL
	if c.baseURL == "" {
		c.baseURL = c.serverURL()
	}
	c.res.BaseURL = c.baseURL

	paths, _ := doc["paths"].(obj)
	pathKeys := make([]string, 0, len(paths))
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	for _, p := range pathKeys {
		item, _ := c.resolve(paths[p]).(obj)
		for _, m := range methodOrder {
			if op, ok := item[m].(obj); ok {
				c.res.Requests = append(c.res.Requests, c.operation(p, m, item, op))
			}
		}
	}
	if len(c.res.Requests) == 0 {
		return nil, fmt.Errorf("no operations found in the document")
	}
	return c.res, nil
}

func parseDocument(data []byte) (obj, error) {
	var doc obj
	if err := json.Unmarshal(data, &doc); err == nil {
		return doc, nil
	}
	var y interface{}
	if err := yaml.Unmarshal(data, &y); err != nil {
		return nil, fmt.Errorf("document is neither valid JSON nor YAML: %w", err)
	}
	m, ok := normalize(y).(obj)
	if !ok {
		return nil, fmt.Errorf("document root must be an object")
	}
	return m, nil
}

// normalize converts YAML's map[interface{}]interface{} shapes to map[string]interface{}.
func normalize(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, e := range t {
			t[k] = normalize(e)
		}
		return t
	case map[interface{}]interface{}:
		m := make(obj, len(t))
		for k, e := range t {
			m[fmt.Sprint(k)] = normalize(e)
		}
		return m
	case []interface{}:
		for i, e := range t {
			t[i] = normalize(e)
		}
		return t
	}
	return v
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

type converter struct {
	doc     obj
	v2      bool
	baseURL string
	res     *Result
}

func (c *converter) warn(format string, a ...interface{}) {
	c.res.Warnings = append(c.res.Warnings, fmt.Sprintf(format, a...))
}

// serverURL derives the base URL from servers (v3) or host/basePath (v2).
func (c *converter) serverURL() string {
	if c.v2 {
		basePath := strings.TrimRight(str(c.doc["basePath"]), "/")
		host := str(c.doc["host"])
		if host == "" {
			return "{{baseUrl}}" + basePath
		}
		scheme := "https"
		if schemes, ok := c.doc["schemes"].([]interface{}); ok && len(schemes) > 0 {
			scheme = str(schemes[0])
			for _, s := range schemes {
				if str(s) == "https" {
					scheme = "https"
				}
			}
		}
		return scheme + "://" + host + basePath
	}

	servers, _ := c.doc["servers"].([]interface{})
	if len(servers) == 0 {
		return "{{baseUrl}}"
	}
	srv, _ := servers[0].(obj)
	raw := str(srv["url"])
	if vars, ok := srv["variables"].(obj); ok {
		for name, def := range vars {
			if d, ok := def.(obj); ok {
				raw = strings.ReplaceAll(raw, "{"+name+"}", fmt.Sprint(d["default"]))
			}
		}
	}
	raw = strings.TrimRight(raw, "/")
	if u, err := url.Parse(raw); err != nil || u.Host == "" {
		return "{{baseUrl}}" + raw // relative server URL
	}
	return raw
}

// resolve follows a local $ref (bounded, to survive reference cycles).
func (c *converter) resolve(v interface{}) interface{} {
	for i := 0; i < 20; i++ {
		m, ok := v.(obj)
		if !ok {
			return v
		}
		ref := str(m["$ref"])
		if ref == "" {
			return v
		}
		if !strings.HasPrefix(ref, "#/") {
			c.warn("external reference %s is not supported", ref)
			return obj{}
		}
		var cur interface{} = c.doc
		for _, part := range strings.Split(ref[2:], "/") {
			part = strings.NewReplacer("~1", "/", "~0", "~").Replace(part)
			parent, isObj := cur.(obj)
			next, ok := parent[part]
			if !isObj || !ok {
				c.warn("unresolved reference %s", ref)
				return obj{}
			}
			cur = next
		}
		v = cur
	}
	return obj{}
}

type param struct {
	name, in string
	required bool
}

func (c *converter) parameters(item, op obj) []param {
	seen := map[string]int{}
	var out []param
	for _, list := range []interface{}{item["parameters"], op["parameters"]} {
		arr, _ := list.([]interface{})
		for _, raw := range arr {
			p, _ := c.resolve(raw).(obj)
			name, in := str(p["name"]), str(p["in"])
			if name == "" || in == "" {
				continue
			}
			req, _ := p["required"].(bool)
			if in == "path" {
				req = true
			}
			key := in + ":" + name
			if i, ok := seen[key]; ok {
				out[i] = param{name, in, req} // operation-level overrides path-level
				continue
			}
			seen[key] = len(out)
			out = append(out, param{name, in, req})
		}
	}
	return out
}

func (c *converter) operation(path, method string, item, op obj) model.SavedRequest {
	name := str(op["operationId"])
	if name == "" {
		name = str(op["summary"])
	}
	if name == "" {
		name = strings.ToUpper(method) + " " + path
	}

	headers := map[string]string{}
	var query []string

	// Path templates like /pets/{id} become {{variables}}, whether or not the
	// parameter is declared
	urlPath := pathTemplate.ReplaceAllString(path, "{{$1}}")
	for _, p := range c.parameters(item, op) {
		switch {
		case p.in == "query" && p.required:
			query = append(query, p.name+"={{"+p.name+"}}")
		case p.in == "header" && p.required:
			headers[p.name] = "{{" + p.name + "}}"
		}
	}

	c.applySecurity(op, headers, &query)

	body := ""
	if c.v2 {
		body = c.v2Body(item, op, headers)
	} else if rb, ok := c.resolve(op["requestBody"]).(obj); ok {
		body = c.v3Body(rb, headers, name)
	}

	full := c.baseURL + urlPath
	if len(query) > 0 {
		full += "?" + strings.Join(query, "&")
	}

	req := model.SavedRequest{
		Name:    name,
		Method:  strings.ToUpper(method),
		URL:     full,
		Headers: headers,
		Body:    body,
	}
	if code := firstSuccessStatus(op); code != "" {
		req.Assertions = []string{"status=" + code}
	}
	return req
}

// firstSuccessStatus returns the lowest documented 2xx status code, if any.
func firstSuccessStatus(op obj) string {
	responses, _ := op["responses"].(obj)
	best := 0
	for k := range responses {
		if n, err := strconv.Atoi(k); err == nil && n >= 200 && n < 300 && (best == 0 || n < best) {
			best = n
		}
	}
	if best == 0 {
		return ""
	}
	return strconv.Itoa(best)
}

// applySecurity adds placeholder credentials for the operation's (or the
// document's) first security requirement.
func (c *converter) applySecurity(op obj, headers map[string]string, query *[]string) {
	reqs, ok := op["security"].([]interface{})
	if !ok {
		reqs, _ = c.doc["security"].([]interface{})
	}
	if len(reqs) == 0 {
		return
	}
	first, _ := reqs[0].(obj)
	names := make([]string, 0, len(first))
	for n := range first {
		names = append(names, n)
	}
	sort.Strings(names)

	var schemes obj
	if c.v2 {
		schemes, _ = c.doc["securityDefinitions"].(obj)
	} else if comps, ok := c.doc["components"].(obj); ok {
		schemes, _ = comps["securitySchemes"].(obj)
	}

	for _, n := range names {
		s, _ := c.resolve(schemes[n]).(obj)
		switch strings.ToLower(str(s["type"])) {
		case "http":
			switch strings.ToLower(str(s["scheme"])) {
			case "bearer":
				headers["Authorization"] = "Bearer {{token}}"
			case "basic":
				headers["Authorization"] = "Basic {{credentials}}"
			}
		case "basic": // Swagger 2
			headers["Authorization"] = "Basic {{credentials}}"
		case "oauth2", "openidconnect":
			headers["Authorization"] = "Bearer {{token}}"
		case "apikey":
			key := str(s["name"])
			switch str(s["in"]) {
			case "header":
				headers[key] = "{{apiKey}}"
			case "query":
				*query = append(*query, key+"={{apiKey}}")
			}
		}
	}
}

func (c *converter) v3Body(rb obj, headers map[string]string, opName string) string {
	content, _ := rb["content"].(obj)
	if len(content) == 0 {
		return ""
	}
	mt := ""
	for _, candidate := range []string{"application/json"} {
		if _, ok := content[candidate]; ok {
			mt = candidate
		}
	}
	if mt == "" {
		keys := make([]string, 0, len(content))
		for k := range content {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.Contains(k, "json") {
				mt = k
				break
			}
		}
		if mt == "" {
			mt = keys[0]
		}
	}
	media, _ := content[mt].(obj)
	headers["Content-Type"] = mt

	if !strings.Contains(mt, "json") {
		if ex, ok := media["example"].(string); ok {
			return ex
		}
		c.warn("%s: %s body not generated (only JSON bodies are sketched)", opName, mt)
		return ""
	}
	if ex, ok := media["example"]; ok {
		return marshal(ex)
	}
	if exs, ok := media["examples"].(obj); ok {
		names := make([]string, 0, len(exs))
		for n := range exs {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) > 0 {
			if e, ok := c.resolve(exs[names[0]]).(obj); ok {
				if v, ok := e["value"]; ok {
					return marshal(v)
				}
			}
		}
	}
	if schema, ok := media["schema"]; ok {
		return marshal(c.sample(schema, 0))
	}
	return ""
}

func (c *converter) v2Body(item, op obj, headers map[string]string) string {
	for _, list := range []interface{}{op["parameters"], item["parameters"]} {
		arr, _ := list.([]interface{})
		for _, raw := range arr {
			p, _ := c.resolve(raw).(obj)
			if str(p["in"]) != "body" {
				continue
			}
			ct := "application/json"
			if consumes, ok := op["consumes"].([]interface{}); ok && len(consumes) > 0 {
				ct = str(consumes[0])
			} else if consumes, ok := c.doc["consumes"].([]interface{}); ok && len(consumes) > 0 {
				ct = str(consumes[0])
			}
			headers["Content-Type"] = ct
			if !strings.Contains(ct, "json") {
				return ""
			}
			return marshal(c.sample(p["schema"], 0))
		}
	}
	return ""
}

func marshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

const maxSampleDepth = 6

// sample builds an example value for a schema.
func (c *converter) sample(schema interface{}, depth int) interface{} {
	s, _ := c.resolve(schema).(obj)
	if depth > maxSampleDepth || s == nil {
		return nil
	}
	if v, ok := s["example"]; ok {
		return v
	}
	if v, ok := s["default"]; ok {
		return v
	}
	if enum, ok := s["enum"].([]interface{}); ok && len(enum) > 0 {
		return enum[0]
	}
	if all, ok := s["allOf"].([]interface{}); ok {
		merged := obj{}
		for _, part := range all {
			if m, ok := c.sample(part, depth+1).(obj); ok {
				for k, v := range m {
					merged[k] = v
				}
			}
		}
		return merged
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if alts, ok := s[key].([]interface{}); ok && len(alts) > 0 {
			return c.sample(alts[0], depth+1)
		}
	}

	typ := str(s["type"])
	if typ == "" {
		if _, ok := s["properties"]; ok {
			typ = "object"
		}
	}
	switch typ {
	case "object":
		out := obj{}
		props, _ := s["properties"].(obj)
		for k, v := range props {
			out[k] = c.sample(v, depth+1)
		}
		return out
	case "array":
		return []interface{}{c.sample(s["items"], depth+1)}
	case "integer":
		return 0
	case "number":
		return 0
	case "boolean":
		return false
	case "string":
		switch str(s["format"]) {
		case "date-time":
			return "2024-01-01T00:00:00Z"
		case "date":
			return "2024-01-01"
		case "uuid":
			return "00000000-0000-0000-0000-000000000000"
		case "email":
			return "user@example.com"
		}
		return "string"
	}
	return nil
}
