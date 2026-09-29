package cmd

import (
	"reflect"
	"testing"

	"api/internal/model"
	"api/internal/storage"
)

func TestVariablesUsed(t *testing.T) {
	got := variablesUsed([]model.SavedRequest{
		{URL: "{{baseUrl}}/pets/{{petId}}", Headers: map[string]string{"Authorization": "Bearer {{token}}"}},
		{URL: "{{baseUrl}}/x", Body: `{"a":"{{petId}}"}`, Headers: map[string]string{"X-{{h}}": "1"}},
	})
	want := []string{"baseUrl", "h", "petId", "token"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSaveImportedRequests_RedactsButKeepsPlaceholders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveImportedRequests("imp", []model.SavedRequest{
		{Name: "a", Method: "GET", URL: "http://x/a", Headers: map[string]string{
			"Authorization": "Bearer {{token}}",
			"Cookie":        "session=abc",
			"Accept":        "*/*",
		}},
	})

	store, err := storage.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	col, _ := store.GetCollection("imp")
	if col == nil || len(col.Requests) != 1 {
		t.Fatalf("collection: %+v", col)
	}
	h := col.Requests[0].Headers
	if h["Authorization"] != "Bearer {{token}}" || h["Cookie"] != redactedValue || h["Accept"] != "*/*" {
		t.Errorf("headers: %v", h)
	}
}
