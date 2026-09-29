package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"api/internal/model"
	"api/internal/vars"
)

func resetVarFlags() {
	headers, data, queryParams = nil, "", nil
	envFlag, varOverride, captureSpec = "", nil, nil
	resetTransportFlags()
}

func TestBuildRequest_TemplateAndSent(t *testing.T) {
	defer resetVarFlags()
	headers = []string{"X-Id: {{id}}"}
	queryParams = []string{"q={{term}}"}
	bearerToken = "{{tok}}"

	tmpl, err := buildRequest("https://api.test/{{id}}", `{"a":"{{id}}"}`, identity)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.url != "https://api.test/{{id}}?q={{term}}" || tmpl.headers["Authorization"] != "Bearer {{tok}}" {
		t.Errorf("template not preserved: %+v", tmpl)
	}

	r := vars.NewResolver(map[string]string{"id": "5", "term": "a b", "tok": "secret"})
	sent, err := buildRequest("https://api.test/{{id}}", `{"a":"{{id}}"}`, r.String)
	if err != nil || r.Err() != nil {
		t.Fatalf("err=%v resolverErr=%v", err, r.Err())
	}
	if sent.url != "https://api.test/5?q=a+b" || sent.body != `{"a":"5"}` ||
		sent.headers["X-Id"] != "5" || sent.headers["Authorization"] != "Bearer secret" {
		t.Errorf("sent wrong: %+v", sent)
	}
}

func TestBuildRequest_MissingVariable(t *testing.T) {
	defer resetVarFlags()
	r := vars.NewResolver(map[string]string{})
	if _, err := buildRequest("https://api.test/{{nope}}", "", r.String); err != nil {
		t.Fatal(err)
	}
	if r.Err() == nil {
		t.Error("expected undefined variable error")
	}
}

func TestCaptureFromResponse_InMemory(t *testing.T) {
	defer resetVarFlags()
	captureSpec = []string{"token=.data.token", "first=.items[0]"}
	v := map[string]string{}
	resp := &model.Response{Body: `{"data":{"token":"abc"},"items":["x"]}`}
	if err := captureFromResponse(resp, v, false); err != nil {
		t.Fatal(err)
	}
	if v["token"] != "abc" || v["first"] != "x" {
		t.Errorf("vars = %v", v)
	}

	captureSpec = []string{"bad name=.x"}
	if err := captureFromResponse(resp, v, false); err == nil {
		t.Error("expected error for invalid name")
	}
	captureSpec = []string{"x=.missing"}
	if err := captureFromResponse(resp, v, false); err != nil {
		t.Errorf("run mode should skip responses lacking the path: %v", err)
	}
	if _, set := v["x"]; set {
		t.Error("variable should not be set")
	}
}

func useCmdForTest() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("clear", false, "")
	return c
}

func TestLoadVariables_EnvAndOverride(t *testing.T) {
	defer resetVarFlags()
	t.Setenv("HOME", t.TempDir())

	// storage via the same code path the commands use
	runEnvCreate(nil, []string{"dev"})
	runEnvSet(nil, []string{"dev", "a=1", "b=2"})
	runEnvUse(useCmdForTest(), []string{"dev"})

	varOverride = []string{"b=override", "c=3"}
	got, err := loadVariables()
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "override" || got["c"] != "3" {
		t.Errorf("vars = %v", got)
	}

	envFlag = "missing"
	if _, err := loadVariables(); err == nil {
		t.Error("expected error for unknown --env")
	}
}
