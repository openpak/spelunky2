package main

import (
	"bytes"
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
	for _, path := range []string{"/get", "/put", "/64b", "/nope"} {
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

func fakeIdentity(t *testing.T, status int) (identityClient, func()) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/switch/identity" {
			t.Errorf("identity lookup went to %s", r.URL.Path)
		}
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write([]byte(`{"pid":123,"baas_user_id":"KU_test","nickname":"tester"}`))
		}
	}))
	return identityClient{url: upstream.URL}, upstream.Close
}

func TestNsoRejectsGarbage(t *testing.T) {
	ids, done := fakeIdentity(t, 401)
	defer done()
	s := newServer(ids)
	req := httptest.NewRequest(http.MethodPost, "https://x.spelunky2.net/nso", strings.NewReader("bogus"))
	rec := httptest.NewRecorder()
	s.route(rec, req)
	if rec.Code != 200 {
		t.Fatalf("got %d, want 200 with the reject packet", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("content type %q, want octet-stream", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), rejectPacket) {
		t.Fatalf("got %x, want the measured reject packet", rec.Body.Bytes())
	}
}

func TestNsoRejectPacketMatchesMossmouth(t *testing.T) {
	if len(rejectPacket) != 18 {
		t.Fatalf("reject packet is %d bytes, measured 18", len(rejectPacket))
	}
	if !bytes.Equal(rejectPacket[:16], make([]byte, 16)) ||
		rejectPacket[16] != 0xff || rejectPacket[17] != 0x00 {
		t.Fatalf("reject packet is not the measured 16 zeros + ff00: %x", rejectPacket)
	}
}

func TestNsoAcceptsAVerifiedToken(t *testing.T) {
	ids, done := fakeIdentity(t, 200)
	defer done()
	s := newServer(ids)
	post := func() []byte {
		req := httptest.NewRequest(http.MethodPost, "https://x.spelunky2.net/nso", strings.NewReader("valid-token"))
		rec := httptest.NewRecorder()
		s.route(rec, req)
		if rec.Code != 200 {
			t.Fatalf("got %d, want 200", rec.Code)
		}
		return rec.Body.Bytes()
	}
	first, second := post(), post()
	for _, got := range [][]byte{first, second} {
		if len(got) != 18 {
			t.Fatalf("success packet is %d bytes, want 18", len(got))
		}
		if bytes.Equal(got[:16], make([]byte, 16)) {
			t.Fatalf("success packet carries a zero session: %x", got)
		}
		if got[16] != 0x00 || got[17] != 0x00 {
			t.Fatalf("success packet status is %x%x, want 0000", got[16], got[17])
		}
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("same token earned two sessions: %x vs %x", first, second)
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
