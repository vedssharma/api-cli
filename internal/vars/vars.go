// Package vars substitutes {{name}} placeholders in request fields.
package vars

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\-]+)\s*\}\}`)

// Substitute replaces every {{name}} in s with its value from vars.
// Names with no value are left untouched and returned in missing.
func Substitute(s string, vars map[string]string) (result string, missing []string) {
	result = placeholder.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		missing = append(missing, name)
		return m
	})
	return result, missing
}

// Resolver substitutes variables and remembers which ones were undefined.
type Resolver struct {
	Vars    map[string]string
	missing map[string]bool
}

// NewResolver returns a Resolver backed by vars.
func NewResolver(vars map[string]string) *Resolver {
	return &Resolver{Vars: vars, missing: map[string]bool{}}
}

// String substitutes variables in s.
func (r *Resolver) String(s string) string {
	out, miss := Substitute(s, r.Vars)
	for _, m := range miss {
		r.missing[m] = true
	}
	return out
}

// Map substitutes variables in the values of m (keys are also substituted)
// and returns a new map.
func (r *Resolver) Map(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[r.String(k)] = r.String(v)
	}
	return out
}

// Err returns an error naming all undefined variables seen so far, or nil.
func (r *Resolver) Err() error {
	if len(r.missing) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.missing))
	for n := range r.missing {
		names = append(names, n)
	}
	sort.Strings(names)
	return fmt.Errorf("undefined variable(s): %s", strings.Join(names, ", "))
}

// MapSegments applies fn to every part of s that is not a {{name}}
// placeholder, leaving placeholders intact.
func MapSegments(s string, fn func(string) string) string {
	var b strings.Builder
	last := 0
	for _, loc := range placeholder.FindAllStringIndex(s, -1) {
		b.WriteString(fn(s[last:loc[0]]))
		b.WriteString(s[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(fn(s[last:]))
	return b.String()
}
