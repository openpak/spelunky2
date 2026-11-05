package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeLinker struct {
	calls int
	ok    bool
	err   error
}

func (f *fakeLinker) Linked(context.Context, string) (bool, error) { f.calls++; return f.ok, f.err }

func TestLinkGate(t *testing.T) {
	f := &fakeLinker{ok: true}
	g := newLinkGate(f, 5*time.Minute)
	now := time.Now()
	g.now = func() time.Time { return now }
	if !g.ok("a") || !g.ok("a") || f.calls != 1 {
		t.Fatalf("positive answers are cached: %d", f.calls)
	}
	now = now.Add(6 * time.Minute)
	g.ok("a")
	if f.calls != 2 {
		t.Fatal("ttl")
	}
	f.ok = false
	if g.ok("b") || g.ok("b") || f.calls != 4 {
		t.Fatalf("an unlinked player is refused and never cached: %d", f.calls)
	}
	f.err = errors.New("down")
	if !g.ok("c") {
		t.Fatal("a core outage must not lock players out")
	}
}
