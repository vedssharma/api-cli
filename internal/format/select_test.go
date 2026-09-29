package format

import "testing"

func TestSelectJSON(t *testing.T) {
	body := `{"data":{"items":[{"name":"a"},{"name":"b"}],"n":3},"ok":true}`
	tests := []struct {
		path, want string
	}{
		{".data.items[1].name", "b"},
		{"data.n", "3"},
		{".ok", "true"},
		{".data.items[0]", "{\n  \"name\": \"a\"\n}"},
	}
	for _, tt := range tests {
		got, err := SelectJSON(body, tt.path)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tt.path, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestSelectJSON_Errors(t *testing.T) {
	body := `{"a":[1,2],"s":"x"}`
	for _, path := range []string{".missing", ".a[5]", ".a.b", ".s.x", ".a[x]", ".a[1"} {
		if _, err := SelectJSON(body, path); err == nil {
			t.Errorf("%s: expected error", path)
		}
	}
	if _, err := SelectJSON("not json", ".a"); err == nil {
		t.Error("expected error for non-JSON body")
	}
}
