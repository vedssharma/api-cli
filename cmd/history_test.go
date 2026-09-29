package cmd

import (
	"reflect"
	"testing"

	"api/internal/model"
)

func testHistory() *model.History {
	mk := func(id, method, url string, status int) model.Request {
		return model.Request{ID: id, Method: method, URL: url, Response: &model.Response{StatusCode: status}}
	}
	return &model.History{Requests: []model.Request{
		mk("aaaa1111", "GET", "https://api.test/users", 200),
		mk("bbbb2222", "POST", "https://api.test/users", 201),
		mk("cccc3333", "GET", "https://api.test/Orders/9", 404),
		{ID: "dddd4444", Method: "GET", URL: "https://other.test/x"}, // no response
	}}
}

func TestFindHistoryRequest(t *testing.T) {
	h := testHistory()
	if r, idx, ok := findHistoryRequest(h, "2"); !ok || r.ID != "bbbb2222" || idx != 2 {
		t.Errorf("by index: %v %d %v", r, idx, ok)
	}
	if r, idx, ok := findHistoryRequest(h, "cccc3333"); !ok || idx != 3 || r.URL != "https://api.test/Orders/9" {
		t.Errorf("by id: %v %d %v", r, idx, ok)
	}
	for _, bad := range []string{"0", "9", "zzzz"} {
		if _, _, ok := findHistoryRequest(h, bad); ok {
			t.Errorf("%q should not be found", bad)
		}
	}
}

func TestSearchHistory(t *testing.T) {
	h := testHistory().Requests
	cases := []struct {
		term, method, status string
		want                 []int
	}{
		{"users", "", "", []int{0, 1}},
		{"ORDERS", "", "", []int{2}},
		{"", "post", "", []int{1}},
		{"", "", "4xx", []int{2}},
		{"", "", "2xx", []int{0, 1}},
		{"", "", "201", []int{1}},
		{"users", "GET", "200", []int{0}},
		{"other", "", "", []int{3}},
		{"other", "", "200", nil}, // no response never matches a status filter
		{"nothing", "", "", nil},
	}
	for _, tc := range cases {
		got := searchHistory(h, tc.term, tc.method, tc.status)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v: got %v, want %v", tc, got, tc.want)
		}
	}
}

func TestValidStatusFilter(t *testing.T) {
	for _, ok := range []string{"200", "404", "4xx", "5XX", " 2xx "} {
		if !validStatusFilter(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "99", "600", "6xx", "0xx", "abc", "4x"} {
		if validStatusFilter(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestReplayRequest_DropsRedactedAndAppliesOverrides(t *testing.T) {
	defer resetVarFlags()
	req := &model.Request{
		Method: "POST", URL: "https://api.test/x", Body: `{"a":"{{v}}"}`,
		Headers: map[string]string{
			"Authorization": redactedValue,
			"X-Keep":        "1",
			"Accept":        "old",
			"X-Tpl":         "{{v}}",
		},
	}

	headers = []string{"accept: new"}
	got, missing, err := replayRequest(req, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, []string{"Authorization"}) {
		t.Errorf("missing = %v", missing)
	}
	if _, has := got.headers["Authorization"]; has {
		t.Error("redacted header must not be sent")
	}
	if got.headers["accept"] != "new" || len(got.headers) != 3 {
		t.Errorf("override not applied cleanly: %v", got.headers)
	}
	if got.headers["X-Tpl"] != "{{v}}" || got.body != `{"a":"{{v}}"}` {
		t.Errorf("identity pass must keep placeholders: %+v", got)
	}

	// Supplying auth again clears the "missing" warning and substitutes variables
	bearerToken = "tok"
	sub := func(s string) string {
		if s == "{{v}}" {
			return "V"
		}
		return s
	}
	got, missing, err = replayRequest(req, sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || got.headers["Authorization"] != "Bearer tok" || got.headers["X-Tpl"] != "V" {
		t.Errorf("missing=%v headers=%v", missing, got.headers)
	}
}

func TestFilterSensitiveHeaders_KeepsPlaceholderValues(t *testing.T) {
	in := map[string]string{
		"Authorization": "Bearer {{token}}",
		"X-Api-Key":     "{{key}}",
		"Cookie":        "session={{sid}}", // literal text alongside the variable: redact
		"X-Auth-Token":  "abc",
		"Proxy-Authorization": "Basic {{creds}}",
	}
	got := filterSensitiveHeaders(in)
	if got["Authorization"] != "Bearer {{token}}" || got["X-Api-Key"] != "{{key}}" || got["Proxy-Authorization"] != "Basic {{creds}}" {
		t.Errorf("placeholder-only values should be kept: %v", got)
	}
	if got["Cookie"] != redactedValue || got["X-Auth-Token"] != redactedValue {
		t.Errorf("literal values should be redacted: %v", got)
	}
}
