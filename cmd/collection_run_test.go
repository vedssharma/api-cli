package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httpclient "api/internal/http"
	"api/internal/model"
)

func TestRunSavedRequest_Assertions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Kind", "test")
		w.Write([]byte(`{"id": 7}`))
	}))
	defer srv.Close()
	defer func() { failOnError = false }()

	client := httpclient.NewClient()
	run := func(req model.SavedRequest, v map[string]string) bool {
		req.Method, req.URL = "GET", srv.URL
		return runSavedRequest(client, req, 1, 1, v, false)
	}

	if run(model.SavedRequest{Assertions: []string{"status=200", ".id=7", "header:X-Kind=test"}}, map[string]string{}) {
		t.Error("passing assertions should not fail the request")
	}
	if !run(model.SavedRequest{Assertions: []string{"status=200", ".id=8"}}, map[string]string{}) {
		t.Error("failing assertion should fail the request")
	}
	if !run(model.SavedRequest{Assertions: []string{"bogus"}}, map[string]string{}) {
		t.Error("malformed assertion should fail the request")
	}
	// Variables are substituted in assertions
	if run(model.SavedRequest{Assertions: []string{".id={{want}}"}}, map[string]string{"want": "7"}) {
		t.Error("assertion variable should be substituted")
	}
	// Undefined variable fails without sending
	if !run(model.SavedRequest{Assertions: []string{".id={{missing}}"}}, map[string]string{}) {
		t.Error("undefined variable should fail")
	}
	// --fail treats 4xx/5xx as failures
	failOnError = true
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	if !runSavedRequest(client, model.SavedRequest{Method: "GET", URL: bad.URL}, 1, 1, map[string]string{}, false) {
		t.Error("500 with --fail should fail")
	}
}
