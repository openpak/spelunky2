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
// cdn.spelunky2.net/static/<yyyymmdd>; serving those boards is a separate step. Until then
// the results are logged and kept in memory.

import (
	"encoding/binary"
	"log"
	"net"
	"sync"

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
}

func newLBS(s *sessions) *lbs { return &lbs{sess: s, runs: map[uint64]runResult{}} }

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
		b.mu.Unlock()
		if !seen {
			log.Printf("[spelunky2] lbs: run result %s ticket=%s values=%d,%d,%d", who(p.BaasUserID), redact.Tag(t), r.Values[0], r.Values[1], r.Values[2])
		}
	default:
		log.Printf("[spelunky2] lbs: %d-byte packet from %s", len(pkt), who(p.BaasUserID))
	}
	return nil
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
