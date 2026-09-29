package cmd

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetTransportFlags() {
	queryParams, formFields, multipartArgs = nil, nil, nil
	bearerToken, basicAuth = "", ""
}

func TestAddQueryParams(t *testing.T) {
	got, err := addQueryParams("https://x.test/a?z=1", []string{"a=b c", "k=v"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://x.test/a?z=1&a=b+c&k=v" {
		t.Errorf("got %s", got)
	}
	if _, err := addQueryParams("https://x.test", []string{"novalue"}); err == nil {
		t.Error("expected error for missing '='")
	}
	if got, _ := addQueryParams("https://x.test", nil); got != "https://x.test" {
		t.Errorf("URL changed without params: %s", got)
	}
}

func TestApplyAuthAndBody_Bearer(t *testing.T) {
	defer resetTransportFlags()
	bearerToken = "tok"
	h := map[string]string{}
	if _, err := applyAuthAndBody(h, "", identity); err != nil {
		t.Fatal(err)
	}
	if h["Authorization"] != "Bearer tok" {
		t.Errorf("got %q", h["Authorization"])
	}
}

func TestApplyAuthAndBody_ExplicitHeaderWins(t *testing.T) {
	defer resetTransportFlags()
	bearerToken = "tok"
	h := map[string]string{"authorization": "Custom"}
	if _, err := applyAuthAndBody(h, "", identity); err != nil {
		t.Fatal(err)
	}
	if _, dup := h["Authorization"]; dup || h["authorization"] != "Custom" {
		t.Errorf("explicit header was overridden: %v", h)
	}
}

func TestApplyAuthAndBody_Basic(t *testing.T) {
	defer resetTransportFlags()
	basicAuth = "u:p"
	h := map[string]string{}
	if _, err := applyAuthAndBody(h, "", identity); err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p"))
	if h["Authorization"] != want {
		t.Errorf("got %q", h["Authorization"])
	}
	basicAuth = "nocolon"
	if _, err := applyAuthAndBody(map[string]string{}, "", identity); err == nil {
		t.Error("expected error for basic auth without ':'")
	}
}

func TestApplyAuthAndBody_Conflicts(t *testing.T) {
	defer resetTransportFlags()
	bearerToken, basicAuth = "t", "u:p"
	if _, err := applyAuthAndBody(map[string]string{}, "", identity); err == nil {
		t.Error("expected bearer/basic conflict")
	}
	resetTransportFlags()
	formFields = []string{"a=b"}
	if _, err := applyAuthAndBody(map[string]string{}, "{}", identity); err == nil {
		t.Error("expected data/form conflict")
	}
}

func TestApplyAuthAndBody_Form(t *testing.T) {
	defer resetTransportFlags()
	formFields = []string{"a=1", "b=x y"}
	h := map[string]string{}
	body, err := applyAuthAndBody(h, "", identity)
	if err != nil {
		t.Fatal(err)
	}
	if body != "a=1&b=x+y" || h["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Errorf("body=%q headers=%v", body, h)
	}
}

func TestBuildMultipartBody(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("file-content"), 0600)

	body, ct, err := buildMultipartBody([]string{"name=bob", "upload=@f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Errorf("content type %q", ct)
	}
	for _, want := range []string{`name="name"`, "bob", `name="upload"; filename="f.txt"`, "file-content"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if _, _, err := buildMultipartBody([]string{"f=@../nope"}); err == nil {
		t.Error("expected error for file outside working directory")
	}
}

func TestClientFromFlags(t *testing.T) {
	defer func() { timeoutSecs, proxyURL = 0, "" }()
	timeoutSecs = -1
	if _, err := clientFromFlags(); err == nil {
		t.Error("expected error for negative timeout")
	}
	timeoutSecs, proxyURL = 0, "not a url"
	if _, err := clientFromFlags(); err == nil {
		t.Error("expected error for bad proxy")
	}
}
