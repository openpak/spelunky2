package main

// The edge probe listener: Spelunky 2's gameplay relay discovery.
//
// After a successful /nso the client resolves edge-00 through edge-31 and
// sprays 2-byte UDP probes ({edge_index, 0x14}, 8 rounds of 32 = 256 total)
// at a fixed game port, picking by reply latency. The port is the client's,
// not ours (docs/ports.md): 2816/udp, like nncs's 22360 or Demonware's 3074.
// Longer packets on the session port are gameplay traffic, relayed to the
// other recent sender (dumb relay until /put names the lobby).

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"openpak/spelunky2/redact"
)

// edgeEcho answers probes while the reply shape is being established.
func serveEdgeProbes(addr string, echo bool) {
	for _, a := range strings.Split(addr, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		go serveEdgePort(a, echo)
	}
}

func serveEdgePort(addr string, echo bool) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[spelunky2] edge listener %s: %v (probes go unanswered)", addr, err)
		return
	}
	defer pc.Close()
	log.Printf("[spelunky2] edge probes on %s", pc.LocalAddr())
	peers := newPeerSet()
	buf := make([]byte, 1500)
	for {
		_ = pc.SetReadDeadline(time.Now().Add(time.Hour))
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		tag := redact.Tag(src.String())
		if n == 2 {
			log.Printf("[spelunky2] edge probe src=%s edge=%d magic=%#04x len=%d", tag, buf[0], buf[1], n)
			if echo {
				// Hypothesis: the client wants its probe echoed inside a
				// 20-byte reply (it asks with magic 0x14 = 20, and selects
				// the edge entry by a reply byte & 0x1f). Zeros elsewhere.
				rep := make([]byte, 20)
				rep[0], rep[1] = buf[0], buf[1]
				if _, err := pc.WriteTo(rep, src); err != nil {
					log.Printf("[spelunky2] edge reply failed: %v", err)
				}
			}
		} else {
			// Anything longer is gameplay-phase traffic (handshakes, state):
			// logged whole and relayed to the other recent senders. The edge
			// is a dumb relay: pairing is by recency until /put names the
			// lobby (two players today). Capture-phase verboseness, same
			// class as NXBAAS_TRACE_BODIES: sessions here are random per
			// login and die with it.
			log.Printf("[spelunky2] edge packet src=%s len=%d body=%x", tag, n, buf[:n])
			for _, dst := range peers.others(src.String()) {
				da := mustAddr(dst)
				if da == nil {
					continue
				}
				if _, err := pc.WriteTo(buf[:n], da); err != nil {
					log.Printf("[spelunky2] edge relay failed: %v", err)
				} else {
					log.Printf("[spelunky2] edge relay %s -> %s len=%d", tag, redact.Tag(dst), n)
				}
			}
			peers.seen(src.String())
		}
	}
}

// peerSet remembers recent gameplay senders so each packet reaches the other
// side. Recency pairing is a stand-in for lobby membership until /put names
// the room; with two players it is exact.
type peerSet struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newPeerSet() *peerSet { return &peerSet{last: map[string]time.Time{}} }

func (p *peerSet) seen(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last[addr] = time.Now()
}

func (p *peerSet) others(addr string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for a, t := range p.last {
		if a != addr && time.Since(t) < time.Minute {
			out = append(out, a)
		}
	}
	return out
}

func mustAddr(s string) net.Addr {
	a, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		return nil
	}
	return a
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
