package main

// Sessions minted from verified Nintendo tokens.
//
// POST /nso carries the raw BAAS id_token as its whole body. The real server
// answers binary (application/octet-stream): 18 bytes for a rejected token
// (16 zero bytes + ff 00, measured 2026-10-13 against Mossmouth for every
// invalid input). The working hypothesis, unproven until a client accepts it:
// {session[16], status u16le} with status 0x00ff = rejected and 0x0000 =
// accepted, the session 16 fresh random bytes the later calls present.
// A wrong success shows the client the same error it shows today; the next
// request the client makes (or does not make) settles the shape.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sync"
	"time"
)

var (
	// rejectPacket is Mossmouth's answer to an invalid token, byte for byte.
	rejectPacket = append(bytes.Repeat([]byte{0}, 16), 0xff, 0x00)
	statusOK     = []byte{0x00, 0x00}
)

// sessions remembers the session a token earned, so a client that re-posts
// /nso (retries, second menu entry) gets the same session rather than a new
// identity every time. Losing the table on restart costs one re-login.
type sessions struct {
	mu       sync.Mutex
	byToken  map[string][16]byte
	bySessID map[string]*player
	leases   map[string]string   // session hex -> connectivity lease ID ("" when unleashed)
	byKey    map[[8]byte]*player // first 8 session bytes: the key the client presents later
	store    runStore            // durable copy of byKey, set once the database is up
}

// attach makes the key -> player map durable: it saves every session minted so far and every
// later one (sessions.save), and lbs falls back to it after a restart.
func (st *sessions) attach(rs runStore) {
	st.mu.Lock()
	st.store = rs
	pending := map[[8]byte]string{}
	for k, p := range st.byKey {
		pending[k] = p.BaasUserID
	}
	st.mu.Unlock()
	for k, id := range pending {
		_ = rs.SaveSession(context.Background(), k, id)
	}
}

// save writes a freshly minted session's key to the database (no-op until attach).
func (st *sessions) save(sess [16]byte, p *player) {
	st.mu.Lock()
	rs := st.store
	st.mu.Unlock()
	if rs == nil {
		return
	}
	var k [8]byte
	copy(k[:], sess[:8])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rs.SaveSession(ctx, k, p.BaasUserID); err != nil {
		log.Printf("[spelunky2] session not saved: %v", err)
	}
}

// playerForKey is byKeyIn, then the database for a session minted before a restart.
func (st *sessions) playerForKey(b []byte) (*player, bool) {
	if p, _, ok := st.byKeyIn(b); ok {
		return p, true
	}
	st.mu.Lock()
	rs := st.store
	st.mu.Unlock()
	if rs == nil || len(b) < 8 {
		return nil, false
	}
	var k [8]byte
	copy(k[:], b[:8])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, ok, err := rs.SessionPlayer(ctx, k)
	if err != nil || !ok {
		return nil, false
	}
	return &player{BaasUserID: id}, true
}

func newSessions() *sessions {
	return &sessions{byToken: map[string][16]byte{}, bySessID: map[string]*player{}, leases: map[string]string{}, byKey: map[[8]byte]*player{}}
}

// forToken returns the session for this token, minting and remembering it on
// first sight. ok is false when no session can be minted.
func (st *sessions) forToken(tok string, p *player) (sess [16]byte, ok bool) {
	sum := sha256.Sum256([]byte(tok))
	key := hex.EncodeToString(sum[:])
	st.mu.Lock()
	defer st.mu.Unlock()
	if s, found := st.byToken[key]; found {
		return s, true
	}
	if _, err := rand.Read(sess[:]); err != nil {
		return sess, false
	}
	st.byToken[key] = sess
	st.bySessID[string(sess[:])] = p
	var k [8]byte
	copy(k[:], sess[:8])
	st.byKey[k] = p
	return sess, true
}

// byKeyIn finds the player whose session key appears 8-byte aligned in b. The client puts
// [NSA id][session key] at the front of its UDP lobby-create and report packets and mixes
// both into the 64-byte header of /64b, /get and /put, so an aligned scan finds it in all.
func (st *sessions) byKeyIn(b []byte) (*player, [8]byte, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var k [8]byte
	for i := 0; i+8 <= len(b) && i < 64; i += 8 {
		copy(k[:], b[i:i+8])
		if p, ok := st.byKey[k]; ok {
			return p, k, true
		}
	}
	return nil, k, false
}

// setLease remembers the connectivity lease a session earned ("" when the
// login went unleashed). Kept for the relay phase's pair allocations.
func (st *sessions) setLease(sess [16]byte, lease string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.leases[hex.EncodeToString(sess[:])] = lease
}

// successPacket renders an accepted login: the session, then status zero.
func successPacket(sess [16]byte) []byte {
	out := make([]byte, 0, 18)
	out = append(out, sess[:]...)
	return append(out, statusOK...)
}
