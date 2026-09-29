package cmd

import (
	"reflect"
	"testing"

	"api/internal/model"
	"api/internal/storage"
)

func TestDynamicCompletions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := storage.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	store.AddToCollection("alpha", model.SavedRequest{Method: "GET", URL: "http://x"})
	store.AddToCollection("beta", model.SavedRequest{Method: "GET", URL: "http://x"})
	store.CreateEnvironment("dev")
	store.CreateEnvironment("prod")
	store.CreateAlias("api", "http://x")
	store.AddToHistory(model.Request{ID: "abcd1234", Method: "GET", URL: "http://x/h"})
	store.Close()

	if got := completeCollections("a"); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("collections: %v", got)
	}
	if got := completeCollections(""); len(got) != 2 {
		t.Errorf("collections: %v", got)
	}
	if got := completeEnvironments("p"); !reflect.DeepEqual(got, []string{"prod"}) {
		t.Errorf("environments: %v", got)
	}
	if got := completeAliases(""); !reflect.DeepEqual(got, []string{"api"}) {
		t.Errorf("aliases: %v", got)
	}
	if got := completeHistory("ab"); !reflect.DeepEqual(got, []string{"abcd1234\tGET http://x/h"}) {
		t.Errorf("history: %v", got)
	}
}

func TestRegisterDynamicCompletions_WiresCommands(t *testing.T) {
	registerDynamicCompletions()
	for _, path := range [][]string{
		{"collection", "show"}, {"collection", "run"}, {"env", "use"}, {"alias", "delete"}, {"history", "replay"},
	} {
		c, _, err := rootCmd.Find(path)
		if err != nil || c.ValidArgsFunction == nil {
			t.Errorf("%v: no completion function", path)
		}
	}
}
