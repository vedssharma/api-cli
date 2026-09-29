package curl

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`curl -X POST http://a`, []string{"curl", "-X", "POST", "http://a"}},
		{`-d '{"a": "b c"}'`, []string{"-d", `{"a": "b c"}`}},
		{`-H "X: \"q\" \$v"`, []string{"-H", `X: "q" $v`}},
		{"curl \\\n  -X GET \\\n  http://a", []string{"curl", "-X", "GET", "http://a"}},
		{`a'b c'"d e"f`, []string{"ab cd ef"}},
		{`-d ''`, []string{"-d", ""}},
		{`it\'s`, []string{"it's"}},
		{"  spaced   out ", []string{"spaced", "out"}},
		{`'it'\''s'`, []string{"it's"}},
	}
	for _, tc := range cases {
		got, err := Tokenize(tc.in)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %q err %v, want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{`'open`, `"open`} {
		if _, err := Tokenize(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func parse(t *testing.T, cmd string) *Request {
	t.Helper()
	toks, err := Tokenize(cmd)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(toks)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	return r
}

func TestParse_Basic(t *testing.T) {
	r := parse(t, `curl -X PUT 'https://api.test/u?x=1' -H 'Accept: text/plain' -H "X-A:  b " -d '{"k":1}'`)
	if r.Method != "PUT" || r.URL != "https://api.test/u?x=1" || r.Body != `{"k":1}` {
		t.Errorf("%+v", r)
	}
	if r.Headers["Accept"] != "text/plain" || r.Headers["X-A"] != "b" {
		t.Errorf("headers %v", r.Headers)
	}
}

func TestParse_MethodInference(t *testing.T) {
	if r := parse(t, "curl https://a.test"); r.Method != "GET" {
		t.Errorf("got %s", r.Method)
	}
	if r := parse(t, "curl https://a.test -d x=1"); r.Method != "POST" || r.Headers["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Errorf("got %+v", r)
	}
	if r := parse(t, "curl -I https://a.test"); r.Method != "HEAD" {
		t.Errorf("got %s", r.Method)
	}
	if r := parse(t, "curl -X DELETE https://a.test -d x"); r.Method != "DELETE" {
		t.Errorf("explicit method must win, got %s", r.Method)
	}
}

func TestParse_CombinedAndAttachedShortOptions(t *testing.T) {
	r := parse(t, `curl -sSLXPOST -H'X-A: 1' https://a.test --compressed`)
	if r.Method != "POST" || r.Headers["X-A"] != "1" || r.URL != "https://a.test" {
		t.Errorf("%+v", r)
	}
}

func TestParse_LongEqualsAndURLFlag(t *testing.T) {
	r := parse(t, `curl --request=PATCH --header='X: y' --url https://a.test/p --data-raw '{"a":1}'`)
	if r.Method != "PATCH" || r.Headers["X"] != "y" || r.URL != "https://a.test/p" || r.Body != `{"a":1}` {
		t.Errorf("%+v", r)
	}
}

func TestParse_UserJSONAndGet(t *testing.T) {
	r := parse(t, `curl -u bob:pw --json '{"a":1}' https://a.test`)
	if r.Headers["Authorization"] != "Basic Ym9iOnB3" || r.Headers["Content-Type"] != "application/json" ||
		r.Headers["Accept"] != "application/json" || r.Method != "POST" || r.Body != `{"a":1}` {
		t.Errorf("%+v", r)
	}
	r = parse(t, `curl -G -d a=1 -d b=2 https://a.test/s?z=0`)
	if r.Method != "GET" || r.URL != "https://a.test/s?z=0&a=1&b=2" || r.Body != "" {
		t.Errorf("%+v", r)
	}
}

func TestParse_MultipleDataJoined(t *testing.T) {
	r := parse(t, `curl -d a=1 -d b=2 https://a.test`)
	if r.Body != "a=1&b=2" {
		t.Errorf("body %q", r.Body)
	}
}

func TestParse_Warnings(t *testing.T) {
	r := parse(t, `curl -F file=@x.png -d @body.json -o out.txt https://a.test`)
	if len(r.Warnings) != 3 {
		t.Errorf("warnings: %v", r.Warnings)
	}
	if r.Body != "" {
		t.Errorf("file body must be dropped, got %q", r.Body)
	}
}

func TestParse_SchemeAndErrors(t *testing.T) {
	if r := parse(t, "curl example.com/x"); r.URL != "http://example.com/x" {
		t.Errorf("got %s", r.URL)
	}
	for _, bad := range [][]string{{"curl"}, {"-X"}, {"-H", "nocolon", "http://a"}} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%v: expected error", bad)
		}
	}
}

func TestBuildAndRoundTrip(t *testing.T) {
	got := Build("POST", "https://a.test/x?y=1", map[string]string{"B": "2", "A": "it's"}, `{"n":"o'k"}`)
	want := `curl -X POST 'https://a.test/x?y=1' -H 'A: it'\''s' -H 'B: 2' -d '{"n":"o'\''k"}'`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if Build("GET", "http://a", nil, "") != "curl 'http://a'" || !strings.Contains(Build("HEAD", "http://a", nil, ""), "--head") {
		t.Error("GET/HEAD rendering wrong")
	}

	toks, _ := Tokenize(got)
	r, err := Parse(toks)
	if err != nil || r.Method != "POST" || r.Headers["A"] != "it's" || r.Body != `{"n":"o'k"}` {
		t.Errorf("round trip failed: %+v %v", r, err)
	}
}
