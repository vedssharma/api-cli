package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRequestJSONRoundTrip(t *testing.T) {
	in := Request{
		ID:        "abcd1234",
		Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		Method:    "POST",
		URL:       "https://a.test/x",
		Headers:   map[string]string{"A": "1"},
		Body:      `{"k":1}`,
		Response:  &Response{StatusCode: 201, Status: "201 Created", Headers: map[string]string{"B": "2"}, Body: "ok", DurationMs: 12},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"id"`, `"timestamp"`, `"method"`, `"url"`, `"headers"`, `"body"`, `"response"`, `"status_code"`, `"duration_ms"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("JSON is missing key %s: %s", key, b)
		}
	}
	var out Request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != in.ID || !out.Timestamp.Equal(in.Timestamp) || out.Response == nil ||
		out.Response.StatusCode != 201 || out.Response.Headers["B"] != "2" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestRequestOmitsNilResponse(t *testing.T) {
	b, _ := json.Marshal(Request{ID: "x"})
	if strings.Contains(string(b), `"response"`) {
		t.Errorf("nil response should be omitted: %s", b)
	}
}

func TestSavedRequestAssertionsOmittedWhenEmpty(t *testing.T) {
	b, _ := json.Marshal(SavedRequest{Name: "n", Method: "GET", URL: "http://x"})
	if strings.Contains(string(b), "assertions") {
		t.Errorf("empty assertions should be omitted: %s", b)
	}
	b, _ = json.Marshal(SavedRequest{Name: "n", Assertions: []string{"status=200"}})
	if !strings.Contains(string(b), `"assertions":["status=200"]`) {
		t.Errorf("assertions missing: %s", b)
	}

	// Older JSON without the field still loads
	var sr SavedRequest
	if err := json.Unmarshal([]byte(`{"name":"n","method":"GET","url":"http://x","headers":{},"body":""}`), &sr); err != nil || sr.Assertions != nil {
		t.Errorf("legacy JSON: %+v %v", sr, err)
	}
}
