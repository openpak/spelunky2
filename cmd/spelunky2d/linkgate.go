package main

import (
	"context"
	"log"
	"sync"
	"time"
)

// Link at first use (prds#131): a BAAS user that no active OpenPak account owns gets the reject
// packet at /nso. A positive answer is cached for five minutes; a negative one never is (a player
// who links right after the refusal gets in on the next try); a core outage lets the player in.
type linker interface {
	Linked(ctx context.Context, baas string) (bool, error)
}

type linkGate struct {
	src linker
	ttl time.Duration
	now func() time.Time
	mu  sync.Mutex
	yes map[string]time.Time
}

func newLinkGate(src linker, ttl time.Duration) *linkGate {
	return &linkGate{src: src, ttl: ttl, now: time.Now, yes: map[string]time.Time{}}
}

func (g *linkGate) ok(baas string) bool {
	g.mu.Lock()
	if exp, hit := g.yes[baas]; hit && g.now().Before(exp) {
		g.mu.Unlock()
		return true
	}
	g.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	linked, err := g.src.Linked(ctx, baas)
	if err != nil {
		log.Printf("[spelunky2] link check failed, letting the player in: %v", err)
		return true
	}
	if !linked {
		return false
	}
	g.mu.Lock()
	if len(g.yes) > 20000 {
		g.yes = map[string]time.Time{}
	}
	g.yes[baas] = g.now().Add(g.ttl)
	g.mu.Unlock()
	return true
}
