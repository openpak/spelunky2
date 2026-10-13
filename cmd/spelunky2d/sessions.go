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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
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
}

func newSessions() *sessions {
	return &sessions{byToken: map[string][16]byte{}, bySessID: map[string]*player{}}
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
	return sess, true
}

// successPacket renders an accepted login: the session, then status zero.
func successPacket(sess [16]byte) []byte {
	out := make([]byte, 0, 18)
	out = append(out, sess[:]...)
	return append(out, statusOK...)
}
