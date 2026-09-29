package assert

import (
	"testing"

	"api/internal/model"
)

var resp = &model.Response{
	StatusCode: 201,
	Headers:    map[string]string{"content-type": "application/json", "X-Rate": "5"},
	Body:       `{"data":{"id":5,"name":"bob","tags":["a","b"]},"ok":true}`,
}

func TestCheck(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"status=201", true},
		{"status=200", false},
		{"status!=500", true},
		{"status<400", true},
		{"status>=202", false},
		{"body~bob", true},
		{`body~"no such"`, false},
		{"body!~nope", true},
		{"header:Content-Type~json", true},
		{"header:x-rate=5", true},
		{"header:X-Rate>3", true},
		{"header:Missing=1", false},
		{"header:Missing!=1", true},
		{".data.id=5", true},
		{".data.id>4", true},
		{".data.id<5", false},
		{".data.name=bob", true},
		{".data.name~ob", true},
		{".data.tags[1]=b", true},
		{".ok=true", true},
		{".data.id", true},
		{".data.nope", false},
		{".data.name>1", false}, // not numeric
	}
	for _, tc := range cases {
		a, err := Parse(tc.expr)
		if err != nil {
			t.Errorf("%s: parse error %v", tc.expr, err)
			continue
		}
		got, detail := a.Check(resp)
		if got != tc.want {
			t.Errorf("%s: got %v (%s), want %v", tc.expr, got, detail, tc.want)
		}
		if !got && detail == "" {
			t.Errorf("%s: failed without detail", tc.expr)
		}
	}
}

func TestParse_Errors(t *testing.T) {
	for _, expr := range []string{
		"", "status", "status~2", "status=abc", "body", "header:", "header:X",
		"foo=1", ".a>x", "status=", "=5",
	} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("%q: expected error", expr)
		}
	}
}

func TestParse_ValueWithOperatorChars(t *testing.T) {
	a, err := Parse(`.url="a=b"`)
	if err != nil || a.value != "a=b" {
		t.Errorf("a=%+v err=%v", a, err)
	}
}
