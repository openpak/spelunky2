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
// cdn.spelunky2.net/static/<yyyymmdd>. Results are stored in Postgres (store.go) and counted for rewards. Tickets and
// results live only in the database (no memory fallback: without it this listener is off).
// Only a ticket issued to that session is accepted, so a result is a fact the server saw.

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/openpak/rewards-emit"

	"openpak/spelunky2/redact"
)

type runResult struct {
	Player string
	Ticket uint64
	Values [3]uint64
}

type lbs struct {
	sess  *sessions
	store runStore     // required: tickets and results live only in the database
	rw    *emit.Client // nil: no rewards events
	now   func() time.Time
}

func newLBS(s *sessions, st runStore) *lbs {
	return &lbs{sess: s, store: st, now: time.Now}
}

func (b *lbs) handle(pkt []byte) []byte {
	if len(pkt) < 16 {
		return nil
	}
	p, ok := b.sess.playerForKey(pkt[8:16])
	if !ok {
		log.Printf("[spelunky2] lbs: unknown session key, len=%d", len(pkt))
		return nil
	}
	switch {
	case len(pkt) == 16:
		t := rand64()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := b.store.IssueTicket(ctx, t, p.BaasUserID, b.now())
		cancel()
		if err != nil {
			log.Printf("[spelunky2] lbs: ticket not stored, not answering: %v", err)
			return nil
		}
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
		// pkt[48:80] is the player's typed name: not kept, the account core owns names.
		b.persist(r)
	default:
		log.Printf("[spelunky2] lbs: %d-byte packet from %s", len(pkt), who(p.BaasUserID))
	}
	return nil
}

// persist stores a result and counts it for rewards. The store accepts it only for a ticket
// issued to the sending player and not stored yet, so the 20 copies, a restart and a forged
// ticket all end here without an event.
func (b *lbs) persist(r runResult) {
	at := b.now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inserted, err := b.store.Insert(ctx, storedRun{Ticket: r.Ticket, Player: r.Player, Values: r.Values, Created: at})
	if err != nil {
		log.Printf("[spelunky2] lbs: store result: %v", err)
		return
	}
	if !inserted {
		return
	}
	log.Printf("[spelunky2] lbs: run result %s ticket=%s values=%d,%d,%d", who(r.Player), redact.Tag(r.Ticket), r.Values[0], r.Values[1], r.Values[2])
	if b.rw == nil {
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
