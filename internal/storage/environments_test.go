package storage

import (
	"reflect"
	"testing"
)

func TestEnvironments_CRUD(t *testing.T) {
	s := newTestStorage(t)

	if names, _ := s.ListEnvironments(); len(names) != 0 {
		t.Fatalf("expected none, got %v", names)
	}
	if err := s.SetEnvVar("nope", "k", "v"); err == nil {
		t.Error("expected error setting var in missing environment")
	}

	for _, n := range []string{"prod", "dev"} {
		if err := s.CreateEnvironment(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateEnvironment("dev"); err != nil {
		t.Errorf("re-creating should be a no-op: %v", err)
	}
	names, _ := s.ListEnvironments()
	if !reflect.DeepEqual(names, []string{"dev", "prod"}) {
		t.Errorf("names = %v", names)
	}

	s.SetEnvVar("dev", "base", "http://dev")
	s.SetEnvVar("dev", "base", "http://dev2")
	s.SetEnvVar("dev", "tok", "x")
	vars, err := s.GetEnvVars("dev")
	if err != nil || !reflect.DeepEqual(vars, map[string]string{"base": "http://dev2", "tok": "x"}) {
		t.Errorf("vars = %v, err %v", vars, err)
	}

	if removed, _ := s.UnsetEnvVar("dev", "tok"); !removed {
		t.Error("expected removal")
	}
	if removed, _ := s.UnsetEnvVar("dev", "tok"); removed {
		t.Error("second removal should report false")
	}
}

func TestEnvironments_Active(t *testing.T) {
	s := newTestStorage(t)
	if a, _ := s.GetActiveEnvironment(); a != "" {
		t.Errorf("active = %q", a)
	}
	if err := s.SetActiveEnvironment("missing"); err == nil {
		t.Error("expected error for missing environment")
	}
	s.CreateEnvironment("dev")
	s.SetEnvVar("dev", "k", "v")
	if err := s.SetActiveEnvironment("dev"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetActiveEnvironment(); a != "dev" {
		t.Errorf("active = %q", a)
	}

	// Deleting the active environment clears it and removes its variables
	if err := s.DeleteEnvironment("dev"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetActiveEnvironment(); a != "" {
		t.Errorf("active after delete = %q", a)
	}
	if vars, _ := s.GetEnvVars("dev"); len(vars) != 0 {
		t.Errorf("vars remain: %v", vars)
	}

	s.CreateEnvironment("x")
	s.SetActiveEnvironment("x")
	s.SetActiveEnvironment("")
	if a, _ := s.GetActiveEnvironment(); a != "" {
		t.Errorf("active after clear = %q", a)
	}
}
