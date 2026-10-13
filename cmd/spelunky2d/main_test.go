package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testServer() *server {
	return newServer(identityClient{})
}

func TestStubPathsAnswer404JSON(t *testing.T) {
	s := testServer()
	for _, path := range []string{"/get", "/put", "/nso", "/64b", "/nope"} {
		req := httptest.NewRequest(http.MethodPost, "https://x.spelunky2.net"+path, strings.NewReader(`{"code":"ABCD"}`))
		rec := httptest.NewRecorder()
		s.route(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", path, rec.Code)
		}
		var v map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Errorf("%s: body is not JSON: %v", path, err)
		}
	}
}

func TestRedactHidesCodeAndToken(t *testing.T) {
	got := redactBody([]byte(`{"code":"ABCD-EFGH","platform":"switch","mode":"coop"}`))
	if strings.Contains(got, "ABCD") {
		t.Errorf("join code reached the log: %s", got)
	}
	for _, want := range []string{`"platform":"switch"`, `"mode":"coop"`} {
		if !strings.Contains(got, want) {
			t.Errorf("safe key missing from %s: %s", got, want)
		}
	}
}

func TestRedactHidesJWT(t *testing.T) {
	tok := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature"
	got := redactBody([]byte(`{"nsa_token":"` + tok + `"}`))
	if strings.Contains(got, tok) {
		t.Errorf("token reached the log: %s", got)
	}
}

func TestLogURLHidesQuerySecrets(t *testing.T) {
	u, err := url.Parse("/get?code=ABCD&platform=switch")
	if err != nil {
		t.Fatal(err)
	}
	got := logURL(u)
	if strings.Contains(got, "ABCD") {
		t.Errorf("query code reached the log: %s", got)
	}
	if !strings.Contains(got, "platform=switch") {
		t.Errorf("safe query param missing from %s", got)
	}
}
