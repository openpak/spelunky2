package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeLinks map[string][]string

func (f fakeLinks) SwitchSubjects(_ context.Context, id string) ([]string, error) { return f[id], nil }

func (m *memStore) DeleteAccount(_ context.Context, players []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in := func(p string) bool {
		for _, x := range players {
			if x == p {
				return true
			}
		}
		return false
	}
	n := 0
	for k, r := range m.runs {
		if in(r.Player) {
			delete(m.runs, k)
			n++
		}
	}
	for k, p := range m.tickets {
		if in(p) {
			delete(m.tickets, k)
			n++
		}
	}
	for k, p := range m.sessions {
		if in(p) {
			delete(m.sessions, k)
			n++
		}
	}
	return n, nil
}

const (
	acctA = "11111111-1111-1111-1111-111111111111"
	acctB = "22222222-2222-2222-2222-222222222222"
)

func eraseFixture(t *testing.T) (*server, *memStore) {
	t.Helper()
	ms := &memStore{}
	s := &server{sessions: newSessions(), links: fakeLinks{acctA: {"KU_a"}, acctB: {"KU_b"}}}
	s.sessions.forToken("ta", &player{BaasUserID: "KU_a"})
	s.sessions.forToken("tb", &player{BaasUserID: "KU_b"})
	s.sessions.attach(ms)
	ctx := context.Background()
	now := time.Now()
	for i, p := range []string{"KU_a", "KU_b"} {
		tk := uint64(i + 1)
		_ = ms.IssueTicket(ctx, tk, p, now)
		_, _ = ms.Insert(ctx, storedRun{Ticket: tk, Player: p, Name: "x", Created: now})
	}
	return s, ms
}

func TestEraseRemovesOnlyThatAccount(t *testing.T) {
	s, ms := eraseFixture(t)
	n, err := s.Erase(context.Background(), acctA)
	if err != nil || n == 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(ms.runs) != 1 || ms.runs[2].Player != "KU_b" || len(ms.tickets) != 1 || len(ms.sessions) != 1 {
		t.Fatalf("store after erase: %+v %+v %+v", ms.runs, ms.tickets, ms.sessions)
	}
	if len(s.sessions.byKey) != 1 || len(s.sessions.byToken) != 1 || len(s.sessions.bySessID) != 1 {
		t.Fatal("memory sessions not pruned to B only")
	}
	if n, err := s.Erase(context.Background(), acctA); err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
}

func TestEraseEndpointAuth(t *testing.T) {
	s, _ := eraseFixture(t)
	call := func(key, hdr, id string) int {
		mux := http.NewServeMux()
		mux.HandleFunc("DELETE /internal/accounts/{account_id}/data", s.eraseHandler(key))
		r := httptest.NewRequest("DELETE", "/internal/accounts/"+id+"/data", nil)
		if hdr != "" {
			r.Header.Set("Authorization", hdr)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		key, hdr, id string
		want         int
	}{
		{"", "Bearer k", acctA, 503},
		{"k", "", acctA, 401},
		{"k", "Bearer wrong", acctA, 401},
		{"k", "Bearer k", "not-a-uuid", 400},
		{"k", "Bearer k", acctA, 200},
	} {
		if got := call(c.key, c.hdr, c.id); got != c.want {
			t.Errorf("%+v: got %d", c, got)
		}
	}
}
