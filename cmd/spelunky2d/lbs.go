package main

// legacy-lbs.mmo.gratis:7853, the client's other UDP service (docs/protocol.md §7).
//
// Two packets, both from the client's main socket and both led by [NSA id 8][session key 8]
// (the first 8 bytes of the /nso session):
//
//	16 bytes   "start a ranked run": sent up to five times, a second apart; the answer is
//	           8 bytes, a run ticket. Without one the client shows a connection error.
//	80 bytes   the run's result: [NSA id][key][ticket][3 x u64 the game's numbers][name 32],
//	           sent 20 times a quarter second apart, never answered.
//
// The results feed the daily-challenge boards the client downloads from
// cdn.spelunky2.net/static/<yyyymmdd>. Results are kept in memory and, with
// SPELUNKY2_DATABASE_URL set, stored in Postgres (store.go) and counted for rewards. Only a
// ticket this process issued to that session is accepted, so a result is a fact the server saw.

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/openpak/rewards-emit"

	"openpak/spelunky2/redact"
)

type runResult struct {
	Player string
	Ticket uint64
	Values [3]uint64
	Name   string
}

type lbs struct {
	sess *sessions
	mu   sync.Mutex
	runs map[uint64]runResult // by ticket: the 20 copies collapse to one

	issued map[uint64]string // tickets handed out -> BAAS user id
	order  []uint64          // issue order, oldest first, to bound issued
	store  runStore          // nil: memory only
	rw     *emit.Client      // nil: no rewards events
	now    func() time.Time
}

const maxIssued = 4096

func newLBS(s *sessions) *lbs {
	return &lbs{sess: s, runs: map[uint64]runResult{}, issued: map[uint64]string{}, now: time.Now}
}

func (b *lbs) issue(t uint64, player string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.issued[t] = player
	b.order = append(b.order, t)
	if len(b.order) > maxIssued {
		delete(b.issued, b.order[0])
		b.order = b.order[1:]
	}
}

func (b *lbs) handle(pkt []byte) []byte {
	if len(pkt) < 16 {
		return nil
	}
	p, _, ok := b.sess.byKeyIn(pkt[8:16])
	if !ok {
		log.Printf("[spelunky2] lbs: unknown session key, len=%d", len(pkt))
		return nil
	}
	switch {
	case len(pkt) == 16:
		t := rand64()
		b.issue(t, p.BaasUserID)
		log.Printf("[spelunky2] lbs: run ticket for %s", who(p.BaasUserID))
		var out [8]byte
		binary.LittleEndian.PutUint64(out[:], t)
		return out[:]
	case len(pkt) >= 0x50:
		t := binary.LittleEndian.Uint64(pkt[16:24])
		r := runResult{Player: p.BaasUserID, Ticket: t}
		for i := range r.Values {
			r.Values[i] = binary.LittleEndian.Uint64(pkt[24+8*i:])
		}
		name := pkt[48:80]
		for i, c := range name {
			if c == 0 {
				name = name[:i]
				break
			}
		}
		r.Name = string(name)
		b.mu.Lock()
		_, seen := b.runs[t]
		b.runs[t] = r
		owner, ours := b.issued[t]
		b.mu.Unlock()
		if !seen {
			b.persist(r, ours && owner == p.BaasUserID)
			log.Printf("[spelunky2] lbs: run result %s ticket=%s values=%d,%d,%d", who(p.BaasUserID), redact.Tag(t), r.Values[0], r.Values[1], r.Values[2])
		}
	default:
		log.Printf("[spelunky2] lbs: %d-byte packet from %s", len(pkt), who(p.BaasUserID))
	}
	return nil
}

// persist stores a first-seen result and counts it for rewards. A result whose ticket this
// process did not issue to the sending session is logged only.
func (b *lbs) persist(r runResult, ours bool) {
	if !ours {
		log.Printf("[spelunky2] lbs: result for a ticket not issued to %s, not stored", who(r.Player))
		return
	}
	if b.store == nil {
		return
	}
	at := b.now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inserted, err := b.store.Insert(ctx, storedRun{Ticket: r.Ticket, Player: r.Player, Name: r.Name, Values: r.Values, Created: at})
	if err != nil {
		log.Printf("[spelunky2] lbs: store result: %v", err)
		return
	}
	if !inserted || b.rw == nil {
		return
	}
	id := emit.Identity{Namespace: "switch", Subject: r.Player}
	run := strconv.FormatUint(r.Ticket, 16)
	day := at.UTC().Format("2006-01-02")
	b.rw.Event(emit.Event{ID: "spelunky2:run:" + run, Metric: "spelunky2_ranked_runs", Player: &id, Keys: []string{run}, At: at})
	b.rw.Event(emit.Event{ID: "spelunky2:daily:" + day + ":" + r.Player, Metric: "spelunky2_daily_runs", Player: &id, Keys: []string{day}, At: at})
}

func serveLBS(addr string, b *lbs) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[spelunky2] lbs listener %s: %v", addr, err)
		return
	}
	defer pc.Close()
	log.Printf("[spelunky2] lbs on %s", pc.LocalAddr())
	buf := make([]byte, 2048)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		if rep := b.handle(buf[:n]); rep != nil {
			_, _ = pc.WriteTo(rep, src)
		}
	}
}
