// Command spelunky2d serves Spelunky 2's own online stack for the Switch build
// (01007EC013ABC000, current update v1.28.0).
//
// The game reaches no Nintendo online service beyond the BAAS account it is
// already logged into. Everything multiplayer is Mossmouth/BlitWorks' own
// HTTPS on one host:
//
//	x.spelunky2.net   /get — lobby fetch (the join-by-code UI reads "Enter Code")
//	                  /put — lobby publish (the host writes its lobby)
//	                  /nso — Nintendo token check (the game reads the
//	                         nn::account token cache and nothing else Nintendo)
//	                  /64b — unknown
//
// Gameplay itself never comes here: after the rendezvous the players talk to
// numbered relays (edge-%02d.spelunky2.net) over raw TCP+UDP sockets.
//
// Plain HTTP inside the pod network; Traefik terminates the guest's TLS for
// these names and forwards the stream, the same arrangement as servers/klei,
// servers/t17 and the Among Us front.
//
// The wire contract was read out of the Switch build's own executable
// (v1.28.0) rather than guessed — see docs/protocol.md, and keep that file in
// step with this code. Every endpoint below is a logging stub until the first
// Harbor boot teaches us the request shapes; an unhandled request is logged
// in full (redacted) because the request is the specification for its handler.
package main

import (
	"bytes"
	"flag"
	"io"
	"log"
	"net/http"
	"strings"
)

var (
	addr       = flag.String("addr", ":22660", "HTTP listen address (Traefik terminates TLS in front)")
	healthAddr = flag.String("health-addr", ":22661", "health listen address")
)

func main() {
	flag.Parse()
	s := newServer(newIdentityClient())

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.route)
	go func() {
		h := http.NewServeMux()
		h.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"status": "ok"})
		})
		log.Printf("[spelunky2] health on %s", *healthAddr)
		log.Fatal(http.ListenAndServe(*healthAddr, h))
	}()

	log.Printf("[spelunky2] serving x.spelunky2.net on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

type server struct {
	ids      identityClient
	sessions *sessions
}

func newServer(ids identityClient) *server { return &server{ids: ids, sessions: newSessions()} }

// route dispatches on path first and hostname second. All four API paths ride
// the one x.spelunky2.net name on one port behind Traefik.
func (s *server) route(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	switch {
	case r.URL.Path == "/get":
		s.getLobby(w, r)
	case r.URL.Path == "/put":
		s.putLobby(w, r)
	case r.URL.Path == "/nso":
		s.nso(w, r)
	case r.URL.Path == "/64b":
		s.misc(w, r, "64b")
	default:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		log.Printf("[spelunky2] !! unhandled %s %s%s body=%s", r.Method, host, logURL(r.URL), redactBody(body))
		writeJSON(w, 404, map[string]any{"error": "not found"})
	}
}

// The four handlers below are capture stubs: they log the redacted request so
// the first Harbor boot writes the protocol spec, and answer 404 until the
// shapes in docs/protocol.md are filled in and implemented.

// getLobby is the join path: the guest asks for a lobby by its short code.
func (s *server) getLobby(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	log.Printf("[spelunky2] get %s body=%s", logURL(r.URL), redactBody(body))
	writeJSON(w, 404, map[string]any{"error": "not found"})
}

// putLobby is the host path: the host publishes its lobby for guests to fetch.
func (s *server) putLobby(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	log.Printf("[spelunky2] put %s body=%s", logURL(r.URL), redactBody(body))
	writeJSON(w, 404, map[string]any{"error": "not found"})
}

// nso is the Nintendo token check: the game posts the raw BAAS id_token as
// the whole body and Mossmouth verifies it server-side. Ours is verified
// against nx-baas's /internal/switch/identity instead — an OpenPak token is
// ours to accept — and an accepted login earns the success packet
// (sessions.go). A rejected one gets Mossmouth's own reject packet,
// byte for byte, which is what the client already handles today.
func (s *server) nso(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<12))
	tok := string(bytes.TrimSpace(body))
	p, err := s.ids.byIDToken(tok)
	if err != nil || tok == "" {
		log.Printf("[spelunky2] nso rejected (%v)", shortErr(err))
		writeOctets(w, rejectPacket)
		return
	}
	sess, ok := s.sessions.forToken(tok, p)
	if !ok {
		log.Printf("[spelunky2] nso session mint failed")
		writeOctets(w, rejectPacket)
		return
	}
	log.Printf("[spelunky2] nso accepted player=%s session=%s", who(p.BaasUserID), sessionTag(sess))
	writeOctets(w, successPacket(sess))
}

// misc covers /64b, whose purpose is still unknown.
func (s *server) misc(w http.ResponseWriter, r *http.Request, what string) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	log.Printf("[spelunky2] %s %s body=%s", what, logURL(r.URL), redactBody(body))
	writeJSON(w, 404, map[string]any{"error": "not found"})
}
