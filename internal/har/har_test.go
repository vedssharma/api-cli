package har

import "testing"

const sample = `{"log":{"entries":[
 {"request":{"method":"post","url":"https://a.test/login?x=1",
   "headers":[{"name":":authority","value":"a.test"},{"name":"Host","value":"a.test"},
              {"name":"Content-Length","value":"9"},{"name":"Accept","value":"*/*"},
              {"name":"X-Dup","value":"1"},{"name":"X-Dup","value":"2"}],
   "postData":{"mimeType":"application/json","text":"{\"a\":1}"}}},
 {"request":{"method":"POST","url":"https://a.test/form","headers":[],
   "postData":{"mimeType":"application/x-www-form-urlencoded","params":[{"name":"k","value":"v w"}]}}},
 {"request":{"method":"GET","url":"wss://a.test/socket","headers":[]}},
 {"request":{"method":"GET","url":"https://b.test","headers":[]}}
]}}`

func TestConvert(t *testing.T) {
	res, err := Convert([]byte(sample), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Requests) != 3 || res.Skipped != 1 {
		t.Fatalf("requests=%d skipped=%d", len(res.Requests), res.Skipped)
	}
	a := res.Requests[0]
	if a.Method != "POST" || a.Name != "POST /login" || a.URL != "https://a.test/login?x=1" || a.Body != `{"a":1}` {
		t.Errorf("first: %+v", a)
	}
	if len(a.Headers) != 3 || a.Headers["Accept"] != "*/*" || a.Headers["X-Dup"] != "1, 2" ||
		a.Headers["Content-Type"] != "application/json" {
		t.Errorf("headers: %v", a.Headers)
	}
	if res.Requests[1].Body != "k=v+w" || res.Requests[1].Headers["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Errorf("form: %+v", res.Requests[1])
	}
	if res.Requests[2].Name != "GET /" {
		t.Errorf("root path name: %q", res.Requests[2].Name)
	}
}

func TestConvert_Filter(t *testing.T) {
	res, err := Convert([]byte(sample), "b.test")
	if err != nil || len(res.Requests) != 1 || res.Requests[0].URL != "https://b.test" {
		t.Errorf("%+v %v", res, err)
	}
	if _, err := Convert([]byte(sample), "nomatch"); err == nil {
		t.Error("expected error when nothing matches")
	}
}

func TestConvert_Errors(t *testing.T) {
	for _, in := range []string{"nope", `{"log":{"entries":[]}}`, `{}`} {
		if _, err := Convert([]byte(in), ""); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}
