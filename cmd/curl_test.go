package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"api/internal/model"
)

func TestRequestNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://a.test/users/1?x=2": "GET /users/1",
		"https://a.test":             "GET /",
		"not a url":                  "GET not a url",
	}
	for in, want := range cases {
		if got := requestNameFromURL("GET", in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func newImportCurlForTest() *cobra.Command {
	c := &cobra.Command{}
	registerImportCurl(c)
	return c.Commands()[0]
}

func TestCurlTokens(t *testing.T) {
	c := newImportCurlForTest()
	got, err := curlTokens(c, []string{`curl -X POST 'https://a.test' -d "x y"`})
	if err != nil || len(got) != 6 || got[5] != "x y" {
		t.Errorf("single arg should be tokenized: %q %v", got, err)
	}
	got, _ = curlTokens(c, []string{"curl", "-X", "POST", "x y"})
	if len(got) != 4 || got[3] != "x y" {
		t.Errorf("multiple args must pass through untouched: %q", got)
	}
}

func TestCurlTokens_FlagsAfterQuotedCommand(t *testing.T) {
	c := newImportCurlForTest()
	got, err := curlTokens(c, []string{"curl https://a.test -H 'A: b'", "-c", "mine", "-n", "nm"})
	if err != nil || len(got) != 4 {
		t.Fatalf("tokens %q err %v", got, err)
	}
	if col, _ := c.Flags().GetString("collection"); col != "mine" {
		t.Errorf("collection = %q", col)
	}
	if n, _ := c.Flags().GetString("name"); n != "nm" {
		t.Errorf("name = %q", n)
	}
	if _, err := curlTokens(newImportCurlForTest(), []string{"curl https://a.test", "stray"}); err == nil {
		t.Error("expected error for stray argument after quoted command")
	}
}

func TestRenderCurlExport(t *testing.T) {
	out := renderCurlExport([]curlExportItem{
		{label: "one\nevil", request: model.SavedRequest{Method: "GET", URL: "http://a/1"}},
		{label: "", request: model.SavedRequest{Method: "POST", URL: "http://a/2", Body: "b", Headers: map[string]string{"A": "{{v}}"}}},
	})
	want := "# one evil\ncurl 'http://a/1'\n\ncurl -X POST 'http://a/2' -H 'A: {{v}}' -d 'b'\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if strings.Count(out, "\n#") != 0 {
		t.Error("label newline leaked into a second line")
	}
}
