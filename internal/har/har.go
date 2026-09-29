// Package har converts HTTP Archive (HAR) files into saved requests.
package har

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"api/internal/model"
)

type file struct {
	Log struct {
		Entries []struct {
			Request struct {
				Method   string `json:"method"`
				URL      string `json:"url"`
				Headers  []nv   `json:"headers"`
				PostData *struct {
					MimeType string `json:"mimeType"`
					Text     string `json:"text"`
					Params   []nv   `json:"params"`
				} `json:"postData"`
			} `json:"request"`
		} `json:"entries"`
	} `json:"log"`
}

type nv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// noiseHeaders are added by the browser or transport and are not part of the
// request a user would want to replay.
var noiseHeaders = map[string]bool{
	"host": true, "content-length": true, "connection": true, "accept-encoding": true,
	"upgrade-insecure-requests": true, "sec-fetch-site": true, "sec-fetch-mode": true,
	"sec-fetch-dest": true, "sec-fetch-user": true, "sec-ch-ua": true,
	"sec-ch-ua-mobile": true, "sec-ch-ua-platform": true, "origin": true, "referer": true,
}

// Result is the outcome of a conversion.
type Result struct {
	Requests []model.SavedRequest
	Skipped  int // entries that are not plain HTTP(S) requests
}

// Convert parses a HAR file. If filter is not empty, only requests whose URL
// contains it are kept.
func Convert(data []byte, filter string) (*Result, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("invalid HAR file: %w", err)
	}
	if len(f.Log.Entries) == 0 {
		return nil, fmt.Errorf("no entries found in HAR file")
	}

	res := &Result{}
	for _, e := range f.Log.Entries {
		r := e.Request
		u, err := url.Parse(r.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || r.Method == "" {
			res.Skipped++
			continue
		}
		if filter != "" && !strings.Contains(r.URL, filter) {
			continue
		}

		headers := map[string]string{}
		for _, h := range r.Headers {
			lower := strings.ToLower(h.Name)
			if h.Name == "" || strings.HasPrefix(h.Name, ":") || noiseHeaders[lower] {
				continue
			}
			if _, dup := headers[h.Name]; dup {
				headers[h.Name] += ", " + h.Value
			} else {
				headers[h.Name] = h.Value
			}
		}

		body := ""
		if r.PostData != nil {
			body = r.PostData.Text
			if body == "" && len(r.PostData.Params) > 0 {
				values := url.Values{}
				for _, p := range r.PostData.Params {
					values.Add(p.Name, p.Value)
				}
				body = values.Encode()
			}
			if r.PostData.MimeType != "" && body != "" {
				hasCT := false
				for k := range headers {
					if strings.EqualFold(k, "Content-Type") {
						hasCT = true
					}
				}
				if !hasCT {
					headers["Content-Type"] = r.PostData.MimeType
				}
			}
		}

		path := u.Path
		if path == "" {
			path = "/"
		}
		res.Requests = append(res.Requests, model.SavedRequest{
			Name:    strings.ToUpper(r.Method) + " " + path,
			Method:  strings.ToUpper(r.Method),
			URL:     r.URL,
			Headers: headers,
			Body:    body,
		})
	}
	if len(res.Requests) == 0 {
		return nil, fmt.Errorf("no matching HTTP requests found (%d entries skipped)", res.Skipped)
	}
	return res, nil
}
