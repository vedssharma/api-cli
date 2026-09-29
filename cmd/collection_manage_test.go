package cmd

import (
	"testing"

	"api/internal/model"
)

func TestFindRequestIndex(t *testing.T) {
	col := &model.Collection{Name: "c", Requests: []model.SavedRequest{
		{Name: "login"}, {Name: ""}, {Name: "dup"}, {Name: "dup"},
	}}

	cases := []struct {
		ref     string
		want    int
		wantErr bool
	}{
		{"1", 0, false},
		{"4", 3, false},
		{"login", 0, false},
		{"0", 0, true},
		{"5", 0, true},
		{"nope", 0, true},
		{"dup", 0, true}, // ambiguous
	}
	for _, tc := range cases {
		got, err := findRequestIndex(col, tc.ref)
		if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
			t.Errorf("ref %q: got %d, err %v", tc.ref, got, err)
		}
	}
}
