package main

// The edges: Spelunky 2's gameplay hosts, edge-NN.spelunky2.net.
//
// Probe port (2816/udp, the client's own): after /nso the client sprays 2-byte
// {edge_index, 20} probes at every edge and keeps the round trip per edge. A real edge
// answers with one byte, the edge index (measured 2026-10-24 against edge-01 and edge-02);
// the client reads only that byte.
//
// Session port (29489/udp, hard-wired in the client): each player streams bytes to the
// edge and the edge hands every player the other players' streams (docs/protocol.md §6).
//
//	client -> edge  [session id 8][bytes held of slot 0..3: 4 x u32][u32 off<<8|len][<=124 bytes]
//	edge -> client  [u8 presence<<4 | chunk mask][u24 bytes of the client's stream held]
//	                then per bit of the mask, slot order: [u32 off<<8|len][bytes]
//
// Offsets are 24-bit stream positions. The client keeps a stream chunk only where it
// extends what it has, so resending from its own count is always safe. A client hears
// nothing for 10 s and drops the session.

import (
	"encoding/binary"
	"log"
	"net"
	"strings"
	"time"

	"openpak/spelunky2/redact"
)

const (
	sessionHeader = 8 + 16 + 4
	chunkMax      = 120 // 4 x (4+120) + 4 fits the client's 0x200-byte receive buffer
	streamCap     = 1 << 24
	presenceTTL   = 10 * time.Second
)

// serveEdgeProbes listens on every probe address (comma list) and answers each probe.
func serveEdgeProbes(addr string, w *world) {
	for _, a := range strings.Split(addr, ",") {
		if a = strings.TrimSpace(a); a != "" {
			go serveProbePort(a)
		}
	}
}

func serveProbePort(addr string) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[spelunky2] edge probe listener %s: %v", addr, err)
		return
	}
	defer pc.Close()
	log.Printf("[spelunky2] edge probes on %s", pc.LocalAddr())
	buf := make([]byte, 1500)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil || n < 1 {
			continue
		}
		_, _ = pc.WriteTo(buf[:1], src)
	}
}

// serveEdgeSessions runs the stream hub on the session port.
func serveEdgeSessions(addr string, w *world) {
	if addr == "" {
		return
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[spelunky2] edge session listener %s: %v", addr, err)
		return
	}
	defer pc.Close()
	log.Printf("[spelunky2] edge sessions on %s", pc.LocalAddr())
	buf := make([]byte, 2048)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		ua, ok := src.(*net.UDPAddr)
		if !ok {
			continue
		}
		for _, o := range w.handleSession(buf[:n], ua) {
			_, _ = pc.WriteTo(o.pkt, o.to)
		}
	}
}

type outPacket struct {
	to  *net.UDPAddr
	pkt []byte
}

// handleSession takes one client packet and returns what to send: an answer to the
// sender, and a push to every other player its new bytes are news to.
func (w *world) handleSession(pkt []byte, src *net.UDPAddr) []outPacket {
	if len(pkt) < sessionHeader {
		return nil
	}
	sess := binary.LittleEndian.Uint64(pkt[0:8])
	w.mu.Lock()
	defer w.mu.Unlock()
	m := w.bySess[sess]
	if m == nil || m.lob == nil {
		log.Printf("[spelunky2] edge: unknown session %s from %s", redact.Tag(sess), redact.Tag(src.String()))
		return nil
	}
	if m.edgeAddr == nil || m.edgeAddr.String() != src.String() {
		log.Printf("[spelunky2] edge: %s slot=%d on lobby %s from %s", redact.Tag(m.pid), m.slot, redact.Tag(m.lob.id), redact.Tag(src.String()))
	}
	m.edgeAddr = src
	m.lastSeen = w.now()
	m.inPkts++
	m.inBytes += len(pkt)
	for i := 0; i < 4; i++ {
		m.acked[i] = binary.LittleEndian.Uint32(pkt[8+4*i:])
	}
	grew := false
	hdr := binary.LittleEndian.Uint32(pkt[24:28])
	off, n := int(hdr>>8), int(hdr&0xff)
	data := pkt[sessionHeader:]
	if n > len(data) {
		n = len(data)
	}
	data = data[:n]
	if off <= len(m.stream) && off+n > len(m.stream) && off+n <= streamCap {
		m.stream = append(m.stream, data[len(m.stream)-off:]...)
		grew = true
		m.accepted++
	} else if n > 0 {
		m.rejected++
		if m.rejected <= 5 {
			log.Printf("[spelunky2] edge: %s slot=%d chunk dropped off=%d len=%d held=%d hdr=%x", redact.Tag(m.pid), m.slot, off, n, len(m.stream), pkt[:sessionHeader])
		}
	}
	if m.inPkts <= 3 {
		log.Printf("[spelunky2] edge: %s slot=%d packet #%d len=%d hdr=%x", redact.Tag(m.pid), m.slot, m.inPkts, len(pkt), pkt[8:sessionHeader])
	}
	if now := w.now(); now.Sub(m.lastStat) >= 5*time.Second {
		m.lastStat = now
		log.Printf("[spelunky2] edge stats: %s slot=%d lobby=%s in=%dpk/%dB out=%dpk/%dB stream=%dB chunks ok=%d dropped=%d acked=%v",
			redact.Tag(m.pid), m.slot, redact.Tag(m.lob.id), m.inPkts, m.inBytes, m.outPkts, m.outBytes, len(m.stream), m.accepted, m.rejected, m.acked)
	}

	out := []outPacket{{m.edgeAddr, w.edgePacketLocked(m)}}
	if grew {
		for _, o := range m.lob.slots {
			if o != nil && o != m && o.edgeAddr != nil {
				out = append(out, outPacket{o.edgeAddr, w.edgePacketLocked(o)})
			}
		}
	}
	for _, o := range out {
		for _, s := range m.lob.slots {
			if s != nil && s.edgeAddr == o.to {
				s.outPkts++
				s.outBytes += len(o.pkt)
			}
		}
	}
	return out
}

// edgePacketLocked builds what player m is owed: presence, its own stream count, and the
// next part of every other player's stream past what m holds.
func (w *world) edgePacketLocked(m *member) []byte {
	l := m.lob
	now := w.now()
	var presence, mask byte
	out := make([]byte, 4, 512)
	held := uint32(len(m.stream)) & 0xffffff
	out[1], out[2], out[3] = byte(held), byte(held>>8), byte(held>>16)
	for i, o := range l.slots {
		if o == nil || o.edgeAddr == nil || now.Sub(o.lastSeen) > presenceTTL {
			continue
		}
		presence |= 1 << i
		if o == m {
			continue
		}
		from := int(m.acked[i])
		if from >= len(o.stream) {
			continue
		}
		chunk := o.stream[from:]
		if len(chunk) > chunkMax {
			chunk = chunk[:chunkMax]
		}
		var h [4]byte
		binary.LittleEndian.PutUint32(h[:], uint32(from)<<8|uint32(len(chunk)))
		out = append(out, h[:]...)
		out = append(out, chunk...)
		mask |= 1 << i
	}
	out[0] = presence<<4 | mask
	return out
}
