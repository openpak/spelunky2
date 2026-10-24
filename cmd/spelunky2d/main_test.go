package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testServer() *server {
	return newServer(identityClient{})
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

func TestNsoRegistersConnectivityLease(t *testing.T) {
	ids, done := fakeIdentity(t, 200)
	defer done()
	var sawKey connParticipantKey
	var sawAuth string
	conn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		var req connRegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("register body is not JSON: %v", err)
		}
		sawKey = req.Key
		_, _ = w.Write([]byte(`{"id":"lease-1","expires_at":"2026-10-13T14:00:00Z"}`))
	}))
	defer conn.Close()
	s := newServerWith(ids, &connectivityClient{url: conn.URL, token: "test-token-0123456789abcdef0123456789"})
	req := httptest.NewRequest(http.MethodPost, "https://x.spelunky2.net/nso", strings.NewReader("valid-token"))
	rec := httptest.NewRecorder()
	s.route(rec, req)
	if rec.Code != 200 || len(rec.Body.Bytes()) != 18 {
		t.Fatalf("login failed with connectivity up: %d", rec.Code)
	}
	if sawAuth != "Bearer test-token-0123456789abcdef0123456789" {
		t.Errorf("auth header %q", sawAuth)
	}
	if sawKey.Title != "spelunky2" || sawKey.Player != "KU_test" || sawKey.Component != "rendezvous" ||
		sawKey.Session == "" || sawKey.Generation == "" {
		t.Errorf("participant key %+v", sawKey)
	}
}

func TestNsoSucceedsUnleashedWhenConnectivityDown(t *testing.T) {
	ids, done := fakeIdentity(t, 200)
	defer done()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer down.Close()
	s := newServerWith(ids, &connectivityClient{url: down.URL, token: "test-token-0123456789abcdef0123456789"})
	req := httptest.NewRequest(http.MethodPost, "https://x.spelunky2.net/nso", strings.NewReader("valid-token"))
	rec := httptest.NewRecorder()
	s.route(rec, req)
	if rec.Code != 200 || len(rec.Body.Bytes()) != 18 {
		t.Fatalf("login failed with connectivity down: %d", rec.Code)
	}
	if got := rec.Body.Bytes(); got[16] != 0x00 || got[17] != 0x00 {
		t.Fatalf("not the success packet: %x", got)
	}
}

func TestNsoSkipsConnectivityWhenUnconfigured(t *testing.T) {
	ids, done := fakeIdentity(t, 200)
	defer done()
	s := newServerWith(ids, &connectivityClient{})
	req := httptest.NewRequest(http.MethodPost, "https://x.spelunky2.net/nso", strings.NewReader("valid-token"))
	rec := httptest.NewRecorder()
	s.route(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login failed with connectivity off: %d", rec.Code)
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

func TestConnectivityTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connectivity.token")
	if err := os.WriteFile(path, []byte("file-token-0123456789abcdef0123456789\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONNECTIVITY_URL", "http://connectivity.internal:20150")
	t.Setenv("CONNECTIVITY_TOKEN", "")
	t.Setenv("CONNECTIVITY_TOKEN_FILE", path)
	c := newConnectivityClient()
	if c.token != "file-token-0123456789abcdef0123456789" {
		t.Errorf("token file not read: %q", c.token)
	}
}
