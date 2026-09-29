package vars

import (
	"reflect"
	"strings"
	"testing"
)

func TestSubstitute(t *testing.T) {
	v := map[string]string{"host": "h.test", "id": "7", "a.b-c": "x"}
	got, missing := Substitute("https://{{host}}/u/{{ id }}?k={{a.b-c}}", v)
	if got != "https://h.test/u/7?k=x" || len(missing) != 0 {
		t.Errorf("got %q missing %v", got, missing)
	}
}

func TestSubstitute_Missing(t *testing.T) {
	got, missing := Substitute("{{a}}-{{b}}-{{a}}", map[string]string{"b": "B"})
	if got != "{{a}}-B-{{a}}" {
		t.Errorf("got %q", got)
	}
	if !reflect.DeepEqual(missing, []string{"a", "a"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestSubstitute_NoRecursionAndOddBraces(t *testing.T) {
	got, _ := Substitute("{{a}} {b} {{ }} {{a", map[string]string{"a": "{{b}}"})
	if got != "{{b}} {b} {{ }} {{a" {
		t.Errorf("got %q", got)
	}
}

func TestResolver(t *testing.T) {
	r := NewResolver(map[string]string{"t": "tok"})
	m := r.Map(map[string]string{"Authorization": "Bearer {{t}}", "X-{{n}}": "1"})
	if m["Authorization"] != "Bearer tok" {
		t.Errorf("map = %v", m)
	}
	err := r.Err()
	if err == nil || !strings.Contains(err.Error(), "n") {
		t.Errorf("expected undefined n, got %v", err)
	}
	if NewResolver(nil).Map(nil) != nil {
		t.Error("nil map should stay nil")
	}
	if NewResolver(map[string]string{}).Err() != nil {
		t.Error("no error expected")
	}
}

func TestMapSegments(t *testing.T) {
	got := MapSegments("a b{{x y}}c d", func(s string) string { return strings.ReplaceAll(s, " ", "_") })
	// "{{x y}}" is not a valid placeholder (space in name), so it is escaped
	if got != "a_b{{x_y}}c_d" {
		t.Errorf("got %q", got)
	}
	got = MapSegments("a b{{x}}c d", func(s string) string { return strings.ReplaceAll(s, " ", "_") })
	if got != "a_b{{x}}c_d" {
		t.Errorf("got %q", got)
	}
}
