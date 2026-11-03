package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openpak/rewards-emit"

	"openpak/spelunky2/internal/rankingsapi"
)

// memStore stands in for Postgres: tests "reopen" it by building a new lbs over the same value.
type memStore struct {
	mu       sync.Mutex
	runs     map[uint64]storedRun
	tickets  map[uint64]string
	sessions map[[8]byte]string
}

func (m *memStore) SaveSession(_ context.Context, k [8]byte, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[[8]byte]string{}
	}
	m.sessions[k] = p
	return nil
}

func (m *memStore) SessionPlayer(_ context.Context, k [8]byte) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.sessions[k]
	return p, ok, nil
}

func (m *memStore) IssueTicket(_ context.Context, t uint64, p string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tickets == nil {
		m.tickets = map[uint64]string{}
	}
	m.tickets[t] = p
	return nil
}

func (m *memStore) Insert(_ context.Context, r storedRun) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tickets[r.Ticket] != r.Player {
		return false, nil
	}
	if _, ok := m.runs[r.Ticket]; ok {
		return false, nil
	}
	if m.runs == nil {
		m.runs = map[uint64]storedRun{}
	}
	m.runs[r.Ticket] = r
	return true, nil
}

func (m *memStore) Standings(_ context.Context, day string, field int) ([]standing, error) {
	best := map[string]standing{}
	for _, r := range m.runs {
		if day != "" && r.Created.UTC().Format("2006-01-02") != day {
			continue
		}
		s := int64(r.Values[field])
		if b, ok := best[r.Player]; !ok || s > b.Score {
			best[r.Player] = standing{r.Player, s, r.Created}
		}
	}
	var out []standing
	for _, b := range best {
		out = append(out, b)
	}
	return out, nil
}

func lbsWith(t *testing.T) (*lbs, *memStore, *[]map[string]any, func()) {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var in struct{ Events []map[string]any }
		_ = json.Unmarshal(b, &in)
		mu.Lock()
		got = append(got, in.Events...)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	rw := emit.New(emit.Config{URL: srv.URL, Key: "k", Source: "spelunky2", Flush: 10 * time.Millisecond})
	s := newSessions()
	s.forToken("tok", &player{BaasUserID: "KU_a"})
	ms := &memStore{}
	s.attach(ms)
	l := newLBS(s, ms)
	l.rw = rw
	l.now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	return l, ms, &got, func() { rw.Close(context.Background()); srv.Close() }
}

func sessKey(l *lbs) []byte {
	for k := range l.sess.byKey {
		return append(make([]byte, 8), k[:]...)
	}
	return nil
}

func resultPkt(key []byte, ticket, score uint64) []byte {
	p := make([]byte, 0x50)
	copy(p, key)
	binary.LittleEndian.PutUint64(p[16:], ticket)
	binary.LittleEndian.PutUint64(p[24:], score)
	copy(p[48:], "Ana")
	return p
}

func TestResultStoredAndCountedOnce(t *testing.T) {
	l, ms, got, done := lbsWith(t)
	key := sessKey(l)
	tk := binary.LittleEndian.Uint64(l.handle(key))
	for i := 0; i < 20; i++ { // the client sends 20 copies
		l.handle(resultPkt(key, tk, 5000))
	}
	if len(ms.runs) != 1 || ms.runs[tk].Player != "KU_a" || ms.runs[tk].Values[0] != 5000 || ms.runs[tk].Name != "Ana" {
		t.Fatalf("runs: %+v", ms.runs)
	}
	done()
	if len(*got) != 2 {
		t.Fatalf("want ranked+daily events, got %v", *got)
	}
	m := map[string]string{}
	for _, e := range *got {
		m[e["metric"].(string)] = e["keys"].([]any)[0].(string)
	}
	if m["spelunky2_daily_runs"] != "2026-11-03" || m["spelunky2_ranked_runs"] == "" {
		t.Fatalf("keys: %v", m)
	}
}

func TestForgedTicketNotStored(t *testing.T) {
	l, ms, got, done := lbsWith(t)
	l.handle(resultPkt(sessKey(l), 42, 1))
	done()
	if len(ms.runs) != 0 || len(*got) != 0 {
		t.Fatal("a ticket we never issued was accepted")
	}
}

func TestRestartKeepsEverythingAndCountsOnce(t *testing.T) {
	l, ms, got, done := lbsWith(t)
	key := sessKey(l)
	tk := binary.LittleEndian.Uint64(l.handle(key)) // ticket issued before the "restart"
	// restart: a new process has empty sessions and a new lbs over the same database
	s2 := newSessions()
	s2.attach(ms)
	l2 := newLBS(s2, ms)
	l2.rw = l.rw
	l2.now = l.now
	l2.handle(resultPkt(key, tk, 7000)) // result arrives after the restart
	for i := 0; i < 5; i++ {
		l2.handle(resultPkt(key, tk, 7000))
	}
	// and a second restart replays the same copies
	s3 := newSessions()
	s3.attach(ms)
	l3 := newLBS(s3, ms)
	l3.rw = l.rw
	l3.now = l.now
	l3.handle(resultPkt(key, tk, 7000))
	now := l.now()
	sts, _ := ms.Standings(context.Background(), now.Format("2006-01-02"), 0)
	if len(sts) != 1 || sts[0].Player != "KU_a" || sts[0].Score != 7000 {
		t.Fatalf("board after restart: %+v", sts)
	}
	done()
	if len(*got) != 2 {
		t.Fatalf("events after restarts: %v", *got)
	}
}

func TestNoSessionNoTicket(t *testing.T) {
	l := newLBS(newSessions(), &memStore{})
	if l.handle(make([]byte, 16)) != nil {
		t.Fatal("ticket for an unknown session")
	}
}

func TestBoards(t *testing.T) {
	ms := &memStore{tickets: map[uint64]string{1: "A", 2: "B", 3: "A"}}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	ms.Insert(context.Background(), storedRun{Ticket: 1, Player: "A", Values: [3]uint64{10}, Created: now})
	ms.Insert(context.Background(), storedRun{Ticket: 2, Player: "B", Values: [3]uint64{30}, Created: now})
	ms.Insert(context.Background(), storedRun{Ticket: 3, Player: "A", Values: [3]uint64{50}, Created: now.AddDate(0, 0, -1)})
	h := rankingsapi.Handler("switch", rankSource{ms, 0, func() time.Time { return now }})
	get := func(p string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec.Body.String()
	}
	if b := get("/internal/rankings/boards"); !strings.Contains(b, `"id":"daily"`) || !strings.Contains(b, `"season":"2026-11-03"`) {
		t.Fatal(b)
	}
	d := get("/internal/rankings/boards/daily")
	if !strings.Contains(d, `"total":2`) || strings.Index(d, `"pid":"B"`) > strings.Index(d, `"pid":"A"`) {
		t.Fatal(d)
	}
	if b := get("/internal/rankings/boards/best"); !strings.Contains(b, `"score":50`) {
		t.Fatal(b)
	}
	if !strings.Contains(get("/internal/rankings/boards/nope"), "no such board") {
		t.Fatal("404 expected")
	}
	off := rankingsapi.Handler("switch", rankSource{ms, -1, time.Now})
	rec := httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/rankings/boards", nil))
	if !strings.Contains(rec.Body.String(), `"boards":[]`) {
		t.Fatal(rec.Body.String())
	}
}
