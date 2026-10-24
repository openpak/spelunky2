package main

// The three HTTPS calls after /nso (docs/protocol.md §4). Each body starts with the
// client's 64-byte header, built from the NSA id, the /nso session key, a build constant
// and a clock through a ChaCha-style proof of work; the session key identifies the player.
//
//	/64b   header only; answers 4 bytes, a u32 holding five 5-bit indexes into
//	       "QWERTYUPASDFGHJKLXCVBMN023456789" (low bits first). Mossmouth answered
//	       0x01e678ba to a garbage header. The client asks again every five minutes.
//	/get   header only; answers exactly 0x3620 bytes, the player's stored progress
//	       block, which the client ORs into its own save. Anything else: no merge.
//	/put   header + 0x3620 bytes: the progress block to store.

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
	"log"
	"net/http"
	"sync"
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

// playerCode is the player's stable 25-bit code: the same account always gets the same one.
func playerCode(nsa string) uint32 {
	sum := sha256.Sum256([]byte("spelunky2-code:" + nsa))
	return binary.LittleEndian.Uint32(sum[:4]) & 0x1ffffff
}

func (s *server) caller(w http.ResponseWriter, r *http.Request, path string, max int64) (*player, []byte, bool) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, max))
	if len(body) < reqHeader {
		log.Printf("[spelunky2] %s: short body len=%d", path, len(body))
		w.WriteHeader(200)
		return nil, nil, false
	}
	p, _, ok := s.sessions.byKeyIn(body[:reqHeader])
	if !ok {
		log.Printf("[spelunky2] %s: no session key in the header len=%d", path, len(body))
		w.WriteHeader(200)
		return nil, nil, false
	}
	return p, body, true
}

func (s *server) code(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.caller(w, r, "64b", 4096)
	if !ok {
		return
	}
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], playerCode(p.BaasUserID))
	log.Printf("[spelunky2] 64b %s", who(p.BaasUserID))
	writeOctets(w, out[:])
}

func (s *server) getLobby(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.caller(w, r, "get", 4096)
	if !ok {
		return
	}
	s.progress.mu.Lock()
	b := s.progress.blocks[p.BaasUserID]
	s.progress.mu.Unlock()
	log.Printf("[spelunky2] get %s stored=%v", who(p.BaasUserID), b != nil)
	writeOctets(w, b)
}

func (s *server) putLobby(w http.ResponseWriter, r *http.Request) {
	p, body, ok := s.caller(w, r, "put", reqHeader+progressBlock+1024)
	if !ok {
		return
	}
	if len(body) != reqHeader+progressBlock {
		log.Printf("[spelunky2] put %s: len=%d, want %d", who(p.BaasUserID), len(body), reqHeader+progressBlock)
		writeOctets(w, nil)
		return
	}
	s.progress.mu.Lock()
	s.progress.blocks[p.BaasUserID] = append([]byte(nil), body[reqHeader:]...)
	s.progress.mu.Unlock()
	log.Printf("[spelunky2] put %s stored", who(p.BaasUserID))
	writeOctets(w, nil)
}
