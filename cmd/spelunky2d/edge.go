package main

// The edge probe listener: Spelunky 2's gameplay relay discovery.
//
// After a successful /nso the client resolves edge-00 through edge-31 and
// sprays 2-byte UDP probes ({edge_index, 0x24}, 8 rounds of 32 = 256 total)
// at a fixed game port, picking by reply latency. The port is the client's,
// not ours (docs/ports.md): 2816/udp, like nncs's 22360 or Demonware's 3074.
//
// This listener logs every probe (source tag, index, length) and answers
// nothing yet: the reply shape is still unknown, and a wrong reply poisons
// the client's latency table worse than silence. Silence still teaches us
// the retry cadence and whatever the client tries after the sweep (TCP?
// /put?). Publish 2816/udp on the quadlet and open the provider ingress plus
// the host ACCEPT for it; without those the probes die upstream and this
// listener only proves the local path.

import (
	"log"
	"net"
	"strings"
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
			// logged whole (capped) and unanswered until its shape is known.
			// Capture-phase verboseness, same class as NXBAAS_TRACE_BODIES:
			// sessions here are random per login and die with it.
			log.Printf("[spelunky2] edge packet src=%s len=%d body=%x", tag, n, buf[:min(n, 128)])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
