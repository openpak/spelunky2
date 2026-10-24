package main

// The lobby server: the UDP service the client knows as spelunky-v20.mmo.gratis:11937.
//
// Read out of the v1.28.0 client and checked against the live service on 2026-10-24
// (docs/protocol.md §5). A client sends its state packet every 0.5-1 s until the reply
// names a gameplay session:
//
//	state  [0:8]  lobby: 0 = idle, all ones = create, anything else = join that lobby
//	       [8:12] seq (the client bumps it on every change), [12:16] lobby version
//	       [16]   slot, [17:49] edge round-trip table (ms/4 per edge, 0xff = no answer)
//	       [49:56] zero
//	record [0] name length, [8:16] player id, [16:20] lobby option, [21] platform<<2 | ready,
//	       [22] character, [23:] name; a record is 23+len bytes (one zero byte when empty)
//
// The reply carries [0:8] edge number (1-based, 0 = none yet), [8:16] the player's session
// id on that edge, then the server's copy of the state (lobby id, the client's seq echoed,
// the lobby version, the player's slot, its edge table) padded to 0x48, then the four
// lobby records in slot order. The client drops a reply whose seq is not its own.
//
// The gameplay session is assigned when every player in the lobby is ready (measured
// 2026-10-24 in Harbor: a client given a session at once stops sending state and never
// reaches the edge, so its lobby goes stale). Until then the replies carry the lobby's
// records with no edge and no session, and the clients keep sending their state.

import (
	"crypto/rand"
	"encoding/binary"
	"log"
	"net"
	"sync"
	"time"

	"openpak/spelunky2/redact"
)

const (
	stateLen    = 0x38 // the state part of a client packet, record follows
	replyHeader = 0x48
	maxSlots    = 4
	lobbyCreate = ^uint64(0)
	memberTTL   = 2 * time.Minute // no lobby or edge traffic for this long: the slot is free
)

type member struct {
	pid      uint64
	lob      *lobby
	slot     int
	record   []byte
	pings    [32]byte
	seq      uint32
	sess     uint64
	lastSeen time.Time

	// edge side (edge.go)
	edgeAddr *net.UDPAddr
	stream   []byte    // this player's byte stream, as the edge has it
	acked    [4]uint32 // how much of each slot's stream this player says it has

	// counters, per edge stream (reset when the member leaves its lobby)
	inPkts, outPkts, inBytes, outBytes, accepted, rejected int
	lastStat                                               time.Time
}

type lobby struct {
	id    uint32
	ver   uint32
	edge  byte // 1-based edge number every member is sent to
	slots [maxSlots]*member

	started bool // every player was ready once: sessions are handed out

}

type world struct {
	mu      sync.Mutex
	lobbies map[uint32]*lobby
	byPID   map[uint64]*member
	bySess  map[uint64]*member
	now     func() time.Time
}

func newWorld() *world {
	return &world{lobbies: map[uint32]*lobby{}, byPID: map[uint64]*member{}, bySess: map[uint64]*member{}, now: time.Now}
}

// recordLen is the client's own rule for how long a record is.
func recordLen(first byte) int {
	if first == 0 {
		return 1
	}
	return int((first-1)&0x1f) + 0x18
}

func rand64() uint64 {
	var b [8]byte
	for {
		_, _ = rand.Read(b[:])
		if v := binary.LittleEndian.Uint64(b[:]); v != 0 && v != lobbyCreate {
			return v
		}
	}
}

// bestEdge picks the edge with the lowest round trip in the client's table (edge numbers
// are 1-based on the wire). All edges answer from this one host, so any reachable one works;
// the lowest keeps the number inside what the client resolved.
func bestEdge(pings [32]byte) byte {
	best, at := byte(0xff), -1
	for i, p := range pings {
		if p < best {
			best, at = p, i
		}
	}
	if at < 0 {
		return 2 // edge-01, the first name Mossmouth serves
	}
	return byte(at + 1)
}

// handleState applies one client state packet and returns the reply.
func (w *world) handleState(pkt []byte) []byte {
	if len(pkt) < stateLen+1 {
		return nil
	}
	lobbyField := binary.LittleEndian.Uint64(pkt[0:8])
	seq := binary.LittleEndian.Uint32(pkt[8:12])
	var pings [32]byte
	copy(pings[:], pkt[17:49])
	rec := pkt[stateLen:]
	if n := recordLen(rec[0]); len(rec) >= n {
		rec = rec[:n]
	}
	var pid uint64
	if len(rec) >= 16 {
		pid = binary.LittleEndian.Uint64(rec[8:16])
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.expireLocked()

	m := w.byPID[pid]
	if pid == 0 {
		return idleReply(seq)
	}
	if m == nil {
		m = &member{pid: pid, slot: -1}
		w.byPID[pid] = m
	}
	if len(rec) > 21 && (len(m.record) <= 21 || m.record[21] != rec[21]) {
		log.Printf("[spelunky2] lobby: %s flags=%#x", redact.Tag(pid), rec[21])
	}
	m.record = append(m.record[:0], rec...)
	m.pings = pings
	m.seq = seq
	m.lastSeen = w.now()

	switch {
	case lobbyField == 0:
		w.leaveLocked(m)
	case lobbyField == lobbyCreate:
		if m.lob == nil || m.slot != 0 {
			w.leaveLocked(m)
			w.createLocked(m)
		}
	default:
		if m.lob == nil || !sameLobby(m.lob.id, lobbyField) {
			w.leaveLocked(m)
			w.joinLocked(m, lobbyField)
		}
	}
	if m.lob == nil {
		return idleReply(seq)
	}
	l := m.lob
	if !l.started && l.allReady() {
		l.started = true
		log.Printf("[spelunky2] lobby %s: everyone ready, sessions on edge %d", redact.Tag(l.id), l.edge)
	}
	if l.started && m.sess == 0 {
		m.sess = rand64()
		w.bySess[m.sess] = m
	}
	return w.replyLocked(m)
}

// ready is the record's ready flag ([21] = platform<<2 | ready).
func ready(rec []byte) bool { return len(rec) > 21 && rec[21]&1 != 0 }

// allReady: the game starts once every player in the lobby has readied up. Until then
// replies carry no edge and no session, which keeps the clients sending their state.
func (l *lobby) allReady() bool {
	n := 0
	for _, s := range l.slots {
		if s == nil {
			continue
		}
		if !ready(s.record) {
			return false
		}
		n++
	}
	return n > 0
}

// sameLobby is the client's own comparison: the low five bits of a lobby id do not count.
// The lobby id is the code the client shows ("Your Code", eight hex digits) and the code a
// friend types to join (measured 2026-10-24: the typed code arrives as the lobby field).
func sameLobby(id uint32, field uint64) bool {
	return (uint64(id)^field)>>5 == 0
}

func (w *world) createLocked(m *member) {
	var id uint32
	for id == 0 || w.lobbies[id] != nil {
		id = uint32(rand64())
	}
	l := &lobby{id: id, ver: 1, edge: bestEdge(m.pings)}
	w.lobbies[id] = l
	l.slots[0] = m
	m.lob, m.slot = l, 0
	log.Printf("[spelunky2] lobby %s created by %s edge=%d", redact.Tag(id), redact.Tag(m.pid), l.edge)
}

func (w *world) joinLocked(m *member, field uint64) {
	var l *lobby
	for _, c := range w.lobbies {
		if sameLobby(c.id, field) {
			l = c
			break
		}
	}
	if l == nil {
		log.Printf("[spelunky2] lobby join by %s: no such lobby", redact.Tag(m.pid))
		return
	}
	for i := range l.slots {
		if l.slots[i] == nil {
			l.slots[i] = m
			m.lob, m.slot = l, i
			l.ver++
			log.Printf("[spelunky2] lobby %s joined by %s slot=%d", redact.Tag(l.id), redact.Tag(m.pid), i)
			return
		}
	}
	log.Printf("[spelunky2] lobby %s full, %s turned away", redact.Tag(l.id), redact.Tag(m.pid))
}

func (w *world) leaveLocked(m *member) {
	if m.lob == nil {
		return
	}
	l := m.lob
	l.slots[m.slot] = nil
	l.ver++
	log.Printf("[spelunky2] lobby %s left by %s slot=%d", redact.Tag(l.id), redact.Tag(m.pid), m.slot)
	m.lob, m.slot = nil, -1
	if m.sess != 0 {
		delete(w.bySess, m.sess)
		m.sess = 0
	}
	m.stream, m.acked, m.edgeAddr = nil, [4]uint32{}, nil
	m.inPkts, m.outPkts, m.inBytes, m.outBytes, m.accepted, m.rejected = 0, 0, 0, 0, 0, 0
	empty := true
	for _, s := range l.slots {
		if s != nil {
			empty = false
		}
	}
	if empty {
		delete(w.lobbies, l.id)
	}
}

// expireLocked frees the slots of players nobody has heard from.
func (w *world) expireLocked() {
	now := w.now()
	for pid, m := range w.byPID {
		if now.Sub(m.lastSeen) > memberTTL {
			w.leaveLocked(m)
			delete(w.byPID, pid)
		}
	}
}

func idleReply(seq uint32) []byte {
	out := make([]byte, replyHeader)
	binary.LittleEndian.PutUint32(out[0x18:], seq)
	binary.LittleEndian.PutUint32(out[0x1c:], 1)
	return out
}

func (w *world) replyLocked(m *member) []byte {
	l := m.lob
	out := make([]byte, replyHeader, replyHeader+4*0x37)
	if m.sess != 0 {
		binary.LittleEndian.PutUint64(out[0:], uint64(l.edge))
		binary.LittleEndian.PutUint64(out[8:], m.sess)
	}
	binary.LittleEndian.PutUint64(out[0x10:], uint64(l.id))
	binary.LittleEndian.PutUint32(out[0x18:], m.seq)
	binary.LittleEndian.PutUint32(out[0x1c:], l.ver)
	out[0x20] = byte(m.slot)
	copy(out[0x21:0x41], m.pings[:])
	for _, s := range l.slots {
		if s == nil || len(s.record) == 0 {
			out = append(out, 0)
			continue
		}
		out = append(out, s.record...)
	}
	return out
}

// serveLobby answers state packets on the lobby port.
func serveLobby(addr string, w *world) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[spelunky2] lobby listener %s: %v", addr, err)
		return
	}
	defer pc.Close()
	log.Printf("[spelunky2] lobby server on %s", pc.LocalAddr())
	buf := make([]byte, 2048)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		rep := w.handleState(buf[:n])
		if rep == nil {
			log.Printf("[spelunky2] lobby: short packet from %s len=%d", redact.Tag(src.String()), n)
			continue
		}
		_, _ = pc.WriteTo(rep, src)
	}
}
