package storage

import (
	"testing"
	"time"

	"api/internal/model"
)

// newTestStorage creates a SQLiteStorage backed by a temporary directory.
// The HOME env var is redirected so NewStorage uses an isolated location.
func newTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := NewStorage()
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ─── History ─────────────────────────────────────────────────────────────────

func TestLoadHistory_Empty(t *testing.T) {
	s := newTestStorage(t)
	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Requests) != 0 {
		t.Errorf("expected 0 requests, got %d", len(h.Requests))
	}
}

func TestAddToHistory_SingleRequest(t *testing.T) {
	s := newTestStorage(t)

	req := model.Request{
		ID:        "test0001",
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Method:    "GET",
		URL:       "https://example.com/api",
		Headers:   map[string]string{"Accept": "application/json"},
		Body:      "",
	}

	if err := s.AddToHistory(req); err != nil {
		t.Fatalf("AddToHistory: %v", err)
	}

	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(h.Requests))
	}
	got := h.Requests[0]
	if got.ID != req.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, req.ID)
	}
	if got.Method != req.Method {
		t.Errorf("Method mismatch: got %q, want %q", got.Method, req.Method)
	}
	if got.URL != req.URL {
		t.Errorf("URL mismatch: got %q, want %q", got.URL, req.URL)
	}
}

func TestAddToHistory_WithResponse(t *testing.T) {
	s := newTestStorage(t)

	req := model.Request{
		ID:        "resp0001",
		Timestamp: time.Now().UTC(),
		Method:    "POST",
		URL:       "https://api.example.com/users",
		Headers:   map[string]string{},
		Body:      `{"name":"Alice"}`,
		Response: &model.Response{
			StatusCode: 201,
			Status:     "201 Created",
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"id":1}`,
			DurationMs: 100,
		},
	}

	if err := s.AddToHistory(req); err != nil {
		t.Fatalf("AddToHistory: %v", err)
	}

	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(h.Requests))
	}

	r := h.Requests[0]
	if r.Response == nil {
		t.Fatal("expected response to be present")
	}
	if r.Response.StatusCode != 201 {
		t.Errorf("status code mismatch: got %d, want 201", r.Response.StatusCode)
	}
	if r.Response.Body != `{"id":1}` {
		t.Errorf("response body mismatch: got %q", r.Response.Body)
	}
	if r.Response.DurationMs != 100 {
		t.Errorf("duration mismatch: got %d, want 100", r.Response.DurationMs)
	}
}

func TestAddToHistory_MultipleRequests_OrderedByTime(t *testing.T) {
	s := newTestStorage(t)

	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		req := model.Request{
			ID:        "order00" + string(rune('0'+i)),
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Method:    "GET",
			URL:       "https://example.com/" + string(rune('a'+i)),
			Headers:   map[string]string{},
		}
		if err := s.AddToHistory(req); err != nil {
			t.Fatalf("AddToHistory(%d): %v", i, err)
		}
	}

	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(h.Requests))
	}
	// History is ordered newest-first
	if h.Requests[0].URL != "https://example.com/c" {
		t.Errorf("expected newest request first, got URL %q", h.Requests[0].URL)
	}
}

func TestHistoryLimit_100(t *testing.T) {
	s := newTestStorage(t)

	base := time.Now().UTC()
	for i := 0; i < 105; i++ {
		req := model.Request{
			ID:        "hist" + padInt(i),
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
			Method:    "GET",
			URL:       "https://example.com/" + padInt(i),
			Headers:   map[string]string{},
		}
		if err := s.AddToHistory(req); err != nil {
			t.Fatalf("AddToHistory(%d): %v", i, err)
		}
	}

	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Requests) > 100 {
		t.Errorf("history should be capped at 100, got %d", len(h.Requests))
	}
}

// padInt zero-pads an integer to 4 digits.
func padInt(n int) string {
	s := "0000" + itoa(n)
	return s[len(s)-4:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestClearHistory(t *testing.T) {
	s := newTestStorage(t)

	req := model.Request{
		ID:        "clr00001",
		Timestamp: time.Now().UTC(),
		Method:    "GET",
		URL:       "https://example.com",
		Headers:   map[string]string{},
	}
	if err := s.AddToHistory(req); err != nil {
		t.Fatalf("AddToHistory: %v", err)
	}

	if err := s.ClearHistory(); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}

	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory after clear: %v", err)
	}
	if len(h.Requests) != 0 {
		t.Errorf("expected empty history after clear, got %d requests", len(h.Requests))
	}
}

func TestGetHistoryRequest_ByID(t *testing.T) {
	s := newTestStorage(t)

	req := model.Request{
		ID:        "findme1",
		Timestamp: time.Now().UTC(),
		Method:    "DELETE",
		URL:       "https://example.com/resource/1",
		Headers:   map[string]string{},
	}
	if err := s.AddToHistory(req); err != nil {
		t.Fatalf("AddToHistory: %v", err)
	}

	got, err := s.GetHistoryRequest("findme1")
	if err != nil {
		t.Fatalf("GetHistoryRequest: %v", err)
	}
	if got == nil {
		t.Fatal("expected request, got nil")
	}
	if got.ID != "findme1" {
		t.Errorf("ID mismatch: got %q", got.ID)
	}
	if got.Method != "DELETE" {
		t.Errorf("Method mismatch: got %q", got.Method)
	}
}

func TestGetHistoryRequest_NotFound(t *testing.T) {
	s := newTestStorage(t)

	got, err := s.GetHistoryRequest("nonexistent")
	if err != nil {
		t.Fatalf("GetHistoryRequest: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent ID, got %+v", got)
	}
}

func TestSaveHistory_ReplacesAll(t *testing.T) {
	s := newTestStorage(t)

	// Add some requests first
	for i := 0; i < 3; i++ {
		s.AddToHistory(model.Request{
			ID:        "old0000" + string(rune('0'+i)),
			Timestamp: time.Now().UTC(),
			Method:    "GET",
			URL:       "https://example.com/old",
			Headers:   map[string]string{},
		})
	}

	// SaveHistory should replace everything
	newHistory := &model.History{
		Requests: []model.Request{
			{
				ID:        "new00001",
				Timestamp: time.Now().UTC(),
				Method:    "POST",
				URL:       "https://example.com/new",
				Headers:   map[string]string{},
			},
		},
	}
	if err := s.SaveHistory(newHistory); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	h, err := s.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Requests) != 1 {
		t.Errorf("expected 1 request after SaveHistory, got %d", len(h.Requests))
	}
	if h.Requests[0].ID != "new00001" {
		t.Errorf("unexpected request ID: %q", h.Requests[0].ID)
	}
}

// ─── Collections ─────────────────────────────────────────────────────────────

func TestCreateCollection(t *testing.T) {
	s := newTestStorage(t)

	if err := s.CreateCollection("my-api"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	col, err := s.GetCollection("my-api")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if col == nil {
		t.Fatal("expected collection, got nil")
	}
	if col.Name != "my-api" {
		t.Errorf("Name mismatch: got %q", col.Name)
	}
	if len(col.Requests) != 0 {
		t.Errorf("expected empty collection, got %d requests", len(col.Requests))
	}
}

func TestCreateCollection_Idempotent(t *testing.T) {
	s := newTestStorage(t)

	// Creating the same collection twice should not error
	if err := s.CreateCollection("dup"); err != nil {
		t.Fatalf("first CreateCollection: %v", err)
	}
	if err := s.CreateCollection("dup"); err != nil {
		t.Fatalf("second CreateCollection (duplicate): %v", err)
	}
}

func TestGetCollection_NotFound(t *testing.T) {
	s := newTestStorage(t)

	col, err := s.GetCollection("nonexistent")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if col != nil {
		t.Errorf("expected nil for non-existent collection, got %+v", col)
	}
}

func TestAddToCollection_CreatesCollectionIfNeeded(t *testing.T) {
	s := newTestStorage(t)

	req := model.SavedRequest{
		Name:    "Get Users",
		Method:  "GET",
		URL:     "https://api.example.com/users",
		Headers: map[string]string{"Accept": "application/json"},
		Body:    "",
	}

	// Collection doesn't exist yet — AddToCollection should create it
	if err := s.AddToCollection("auto-create", req); err != nil {
		t.Fatalf("AddToCollection: %v", err)
	}

	col, err := s.GetCollection("auto-create")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if col == nil {
		t.Fatal("expected collection to be created, got nil")
	}
	if len(col.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(col.Requests))
	}
	if col.Requests[0].Name != "Get Users" {
		t.Errorf("Name mismatch: got %q", col.Requests[0].Name)
	}
}

func TestAddToCollection_PreservesOrder(t *testing.T) {
	s := newTestStorage(t)

	if err := s.CreateCollection("ordered"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	methods := []string{"GET", "POST", "PUT", "DELETE"}
	for _, m := range methods {
		if err := s.AddToCollection("ordered", model.SavedRequest{
			Name:    m + " request",
			Method:  m,
			URL:     "https://example.com",
			Headers: map[string]string{},
		}); err != nil {
			t.Fatalf("AddToCollection (%s): %v", m, err)
		}
	}

	col, err := s.GetCollection("ordered")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if len(col.Requests) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(col.Requests))
	}
	for i, m := range methods {
		if col.Requests[i].Method != m {
			t.Errorf("position %d: expected method %q, got %q", i, m, col.Requests[i].Method)
		}
	}
}

func TestAddToCollection_WithBody(t *testing.T) {
	s := newTestStorage(t)

	req := model.SavedRequest{
		Name:    "Create User",
		Method:  "POST",
		URL:     "https://api.example.com/users",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"name":"Bob"}`,
	}

	if err := s.AddToCollection("withbody", req); err != nil {
		t.Fatalf("AddToCollection: %v", err)
	}

	col, err := s.GetCollection("withbody")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if col.Requests[0].Body != `{"name":"Bob"}` {
		t.Errorf("body mismatch: got %q", col.Requests[0].Body)
	}
	if col.Requests[0].Headers["Content-Type"] != "application/json" {
		t.Errorf("header mismatch: got %q", col.Requests[0].Headers["Content-Type"])
	}
}

func TestDeleteCollection(t *testing.T) {
	s := newTestStorage(t)

	if err := s.CreateCollection("to-delete"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := s.AddToCollection("to-delete", model.SavedRequest{
		Method:  "GET",
		URL:     "https://example.com",
		Headers: map[string]string{},
	}); err != nil {
		t.Fatalf("AddToCollection: %v", err)
	}

	if err := s.DeleteCollection("to-delete"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}

	col, err := s.GetCollection("to-delete")
	if err != nil {
		t.Fatalf("GetCollection after delete: %v", err)
	}
	if col != nil {
		t.Errorf("expected nil after delete, got %+v", col)
	}
}

func TestDeleteCollection_NonExistent(t *testing.T) {
	s := newTestStorage(t)
	// Deleting a non-existent collection should not error
	if err := s.DeleteCollection("ghost"); err != nil {
		t.Errorf("unexpected error deleting non-existent collection: %v", err)
	}
}

func TestLoadCollections_MultipleCollections(t *testing.T) {
	s := newTestStorage(t)

	s.CreateCollection("col-a")
	s.CreateCollection("col-b")
	s.AddToCollection("col-a", model.SavedRequest{Method: "GET", URL: "https://a.example.com", Headers: map[string]string{}})
	s.AddToCollection("col-a", model.SavedRequest{Method: "POST", URL: "https://a.example.com", Headers: map[string]string{}})
	s.AddToCollection("col-b", model.SavedRequest{Method: "DELETE", URL: "https://b.example.com", Headers: map[string]string{}})

	cols, err := s.LoadCollections()
	if err != nil {
		t.Fatalf("LoadCollections: %v", err)
	}
	if len(cols.Collections) != 2 {
		t.Errorf("expected 2 collections, got %d", len(cols.Collections))
	}
	if len(cols.Collections["col-a"].Requests) != 2 {
		t.Errorf("expected 2 requests in col-a, got %d", len(cols.Collections["col-a"].Requests))
	}
	if len(cols.Collections["col-b"].Requests) != 1 {
		t.Errorf("expected 1 request in col-b, got %d", len(cols.Collections["col-b"].Requests))
	}
}

func TestSaveCollections_ReplacesAll(t *testing.T) {
	s := newTestStorage(t)

	s.CreateCollection("old-col")
	s.AddToCollection("old-col", model.SavedRequest{Method: "GET", URL: "https://old.example.com", Headers: map[string]string{}})

	newCols := &model.Collections{
		Collections: map[string]model.Collection{
			"new-col": {
				Name: "new-col",
				Requests: []model.SavedRequest{
					{Name: "New Request", Method: "POST", URL: "https://new.example.com", Headers: map[string]string{}},
				},
			},
		},
	}
	if err := s.SaveCollections(newCols); err != nil {
		t.Fatalf("SaveCollections: %v", err)
	}

	cols, err := s.LoadCollections()
	if err != nil {
		t.Fatalf("LoadCollections: %v", err)
	}
	if _, exists := cols.Collections["old-col"]; exists {
		t.Error("old collection should have been replaced")
	}
	if _, exists := cols.Collections["new-col"]; !exists {
		t.Error("new collection should exist")
	}
}

// ─── Aliases ──────────────────────────────────────────────────────────────────

func TestCreateAlias_And_GetAlias(t *testing.T) {
	s := newTestStorage(t)

	if err := s.CreateAlias("myapi", "https://api.example.com"); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}

	url, exists, err := s.GetAlias("myapi")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if !exists {
		t.Fatal("expected alias to exist")
	}
	if url != "https://api.example.com" {
		t.Errorf("URL mismatch: got %q", url)
	}
}

func TestGetAlias_NotFound(t *testing.T) {
	s := newTestStorage(t)

	_, exists, err := s.GetAlias("ghost")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if exists {
		t.Error("expected alias not to exist")
	}
}

func TestCreateAlias_Upsert(t *testing.T) {
	s := newTestStorage(t)

	if err := s.CreateAlias("sw", "https://swapi.dev/api"); err != nil {
		t.Fatalf("first CreateAlias: %v", err)
	}
	// Update with new URL
	if err := s.CreateAlias("sw", "https://swapi.tech/api"); err != nil {
		t.Fatalf("second CreateAlias (upsert): %v", err)
	}

	url, _, err := s.GetAlias("sw")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if url != "https://swapi.tech/api" {
		t.Errorf("expected updated URL, got %q", url)
	}
}

func TestDeleteAlias(t *testing.T) {
	s := newTestStorage(t)

	if err := s.CreateAlias("del-me", "https://example.com"); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	if err := s.DeleteAlias("del-me"); err != nil {
		t.Fatalf("DeleteAlias: %v", err)
	}

	_, exists, err := s.GetAlias("del-me")
	if err != nil {
		t.Fatalf("GetAlias after delete: %v", err)
	}
	if exists {
		t.Error("expected alias to be deleted")
	}
}

func TestDeleteAlias_NonExistent(t *testing.T) {
	s := newTestStorage(t)
	// Should not error
	if err := s.DeleteAlias("ghost"); err != nil {
		t.Errorf("unexpected error deleting non-existent alias: %v", err)
	}
}

func TestLoadAliases_Multiple(t *testing.T) {
	s := newTestStorage(t)

	s.CreateAlias("api1", "https://api1.example.com")
	s.CreateAlias("api2", "https://api2.example.com")
	s.CreateAlias("api3", "https://api3.example.com")

	aliases, err := s.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(aliases.Aliases) != 3 {
		t.Errorf("expected 3 aliases, got %d", len(aliases.Aliases))
	}
	if aliases.Aliases["api1"] != "https://api1.example.com" {
		t.Errorf("unexpected alias value for api1: %q", aliases.Aliases["api1"])
	}
}

func TestLoadAliases_Empty(t *testing.T) {
	s := newTestStorage(t)

	aliases, err := s.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(aliases.Aliases) != 0 {
		t.Errorf("expected 0 aliases, got %d", len(aliases.Aliases))
	}
}

func TestSaveAliases_ReplacesAll(t *testing.T) {
	s := newTestStorage(t)

	s.CreateAlias("old", "https://old.example.com")

	newAliases := &model.Aliases{
		Aliases: map[string]string{
			"new1": "https://new1.example.com",
			"new2": "https://new2.example.com",
		},
	}
	if err := s.SaveAliases(newAliases); err != nil {
		t.Fatalf("SaveAliases: %v", err)
	}

	aliases, err := s.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if _, exists := aliases.Aliases["old"]; exists {
		t.Error("old alias should have been replaced")
	}
	if len(aliases.Aliases) != 2 {
		t.Errorf("expected 2 new aliases, got %d", len(aliases.Aliases))
	}
}

// ─── parseJSONHeaders ─────────────────────────────────────────────────────────

func TestParseJSONHeaders_Valid(t *testing.T) {
	got, err := parseJSONHeaders(`{"Content-Type":"application/json","Accept":"*/*"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["Content-Type"] != "application/json" {
		t.Errorf("unexpected value: %q", got["Content-Type"])
	}
	if got["Accept"] != "*/*" {
		t.Errorf("unexpected value: %q", got["Accept"])
	}
}

func TestParseJSONHeaders_Empty(t *testing.T) {
	got, err := parseJSONHeaders("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseJSONHeaders_EmptyObject(t *testing.T) {
	got, err := parseJSONHeaders("{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for {}, got %v", got)
	}
}

func TestParseJSONHeaders_Invalid(t *testing.T) {
	got, err := parseJSONHeaders("not-json")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
	// Should still return an empty map (not nil)
	if got == nil {
		t.Error("expected non-nil map even on error")
	}
}

// ─── Collection management ───────────────────────────────────────────────────

func seedCollection(t *testing.T, s *SQLiteStorage) {
	t.Helper()
	for _, n := range []string{"a", "b", "c"} {
		if err := s.AddToCollection("col", model.SavedRequest{Name: n, Method: "GET", URL: "http://x/" + n}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenameCollection(t *testing.T) {
	s := newTestStorage(t)
	seedCollection(t, s)
	s.CreateCollection("other")

	if err := s.RenameCollection("col", "other"); err == nil {
		t.Error("expected error renaming onto an existing collection")
	}
	if err := s.RenameCollection("missing", "new"); err == nil {
		t.Error("expected error renaming a missing collection")
	}
	if err := s.RenameCollection("col", "renamed"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.GetCollection("col"); c != nil {
		t.Error("old name should be gone")
	}
	c, _ := s.GetCollection("renamed")
	if c == nil || len(c.Requests) != 3 {
		t.Errorf("requests should move with the collection: %+v", c)
	}
}

func TestRemoveFromCollection(t *testing.T) {
	s := newTestStorage(t)
	seedCollection(t, s)

	if err := s.RemoveFromCollection("col", 1); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetCollection("col")
	if len(c.Requests) != 2 || c.Requests[0].Name != "a" || c.Requests[1].Name != "c" {
		t.Fatalf("unexpected requests: %+v", c.Requests)
	}

	// Positions stay dense: a new request goes last and removal by index still works
	s.AddToCollection("col", model.SavedRequest{Name: "d", Method: "GET", URL: "http://x/d"})
	if err := s.RemoveFromCollection("col", 1); err != nil {
		t.Fatal(err)
	}
	c, _ = s.GetCollection("col")
	if len(c.Requests) != 2 || c.Requests[1].Name != "d" {
		t.Errorf("unexpected requests: %+v", c.Requests)
	}

	if err := s.RemoveFromCollection("col", 5); err == nil {
		t.Error("expected error for out-of-range index")
	}
	if err := s.RemoveFromCollection("col", -1); err == nil {
		t.Error("expected error for negative index")
	}
	if err := s.RemoveFromCollection("missing", 0); err == nil {
		t.Error("expected error for missing collection")
	}
}

func TestUpdateInCollection(t *testing.T) {
	s := newTestStorage(t)
	seedCollection(t, s)

	err := s.UpdateInCollection("col", 1, model.SavedRequest{
		Name: "B2", Method: "POST", URL: "http://x/new", Headers: map[string]string{"A": "1"}, Body: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetCollection("col")
	got := c.Requests[1]
	if got.Name != "B2" || got.Method != "POST" || got.URL != "http://x/new" || got.Headers["A"] != "1" || got.Body != "{}" {
		t.Errorf("update not applied: %+v", got)
	}
	if c.Requests[0].Name != "a" || c.Requests[2].Name != "c" {
		t.Error("neighbouring requests changed")
	}
	if err := s.UpdateInCollection("col", 9, model.SavedRequest{}); err == nil {
		t.Error("expected error for out-of-range index")
	}
}
