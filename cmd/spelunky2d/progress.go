package main

// The three HTTPS calls after /nso (docs/protocol.md §7). Each body starts with the
// client's 64-byte token: made once per save from the NSA id, the /nso session key, a build
// constant and a clock, chained through a ChaCha-style round function until a 25-bit
// proof-of-work condition holds, then kept in the save. Only the top half of the NSA id
// survives the chaining (emulated 2026-10-24), so the token itself is the identity:
//
//	/64b   token only; answers 4 bytes, a u32 holding five 5-bit indexes into
//	       "QWERTYUPASDFGHJKLXCVBMN023456789" (low bits first). Mossmouth answers any
//	       token (0x01e678ba for a garbage one). Ours is a hash of the token, so it is
//	       stable for as long as the save keeps its token.
//	/get   token only; answers exactly 0x3620 bytes, the progress block stored under the
//	       token, which the client ORs into its own save. Anything else: no merge.
//	/put   token + 0x3620 bytes: the progress block to store.

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
	"log"
	"net/http"
	"sync"

	"openpak/spelunky2/redact"
)

const (
	reqHeader     = 64
	progressBlock = 0x3620
)

type progressStore struct {
	mu     sync.Mutex
	blocks map[string][]byte // by NSA id
}

func newProgressStore() *progressStore { return &progressStore{blocks: map[string][]byte{}} }

func tokenKey(tok []byte) string {
	sum := sha256.Sum256(tok)
	return string(sum[:])
}

// tokenCode is the 25-bit code for a token: the same token always gets the same code.
func tokenCode(tok []byte) uint32 {
	sum := sha256.Sum256(append([]byte("spelunky2-code:"), tok...))
	return binary.LittleEndian.Uint32(sum[:4]) & 0x1ffffff
}

func readToken(w http.ResponseWriter, r *http.Request, path string, max int64) ([]byte, bool) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, max))
	if len(body) < reqHeader {
		log.Printf("[spelunky2] %s: short body len=%d", path, len(body))
		writeOctets(w, nil)
		return nil, false
	}
	return body, true
}

func (s *server) code(w http.ResponseWriter, r *http.Request) {
	body, ok := readToken(w, r, "64b", 4096)
	if !ok {
		return
	}
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], tokenCode(body[:reqHeader]))
	log.Printf("[spelunky2] 64b token=%s", redact.Tag(body[:reqHeader]))
	writeOctets(w, out[:])
}

func (s *server) getLobby(w http.ResponseWriter, r *http.Request) {
	body, ok := readToken(w, r, "get", 4096)
	if !ok {
		return
	}
	s.progress.mu.Lock()
	b := s.progress.blocks[tokenKey(body[:reqHeader])]
	s.progress.mu.Unlock()
	log.Printf("[spelunky2] get token=%s stored=%v", redact.Tag(body[:reqHeader]), b != nil)
	writeOctets(w, b)
}

func (s *server) putLobby(w http.ResponseWriter, r *http.Request) {
	body, ok := readToken(w, r, "put", reqHeader+progressBlock+1024)
	if !ok {
		return
	}
	if len(body) != reqHeader+progressBlock {
		log.Printf("[spelunky2] put: len=%d, want %d", len(body), reqHeader+progressBlock)
		writeOctets(w, nil)
		return
	}
	s.progress.mu.Lock()
	s.progress.blocks[tokenKey(body[:reqHeader])] = append([]byte(nil), body[reqHeader:]...)
	s.progress.mu.Unlock()
	log.Printf("[spelunky2] put token=%s stored", redact.Tag(body[:reqHeader]))
	writeOctets(w, nil)
}
