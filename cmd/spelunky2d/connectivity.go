package main

// The shared connectivity service (servers/connectivity): participant leases
// so players behind one public IP stay distinct.
//
// On every accepted /nso this service registers one participant generation
// for the login session: title spelunky2, player the verified BAAS id,
// session and generation both the minted session. The lease ID is kept with
// the session so the relay phase (AllocateRelay for a host/joiner pair, once
// /put and /get show the lobby shape) has IDs to name. Renewal is free: a
// re-posted /nso carries the same key and renews the same ID.
//
// Unconfigured (CONNECTIVITY_URL empty) or failing, registration is skipped
// with one log line and the login still succeeds: the rendezvous must never
// break because the relay bookkeeping is down. Only stdlib here — the shape
// follows servers/connectivity's contract, not its client package.

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

var connHTTP = &http.Client{Timeout: 5 * time.Second}

type connParticipantKey struct {
	Title      string `json:"title"`
	Session    string `json:"session"`
	Player     string `json:"player"`
	Generation string `json:"generation"`
	Component  string `json:"component"`
}

type connRegisterRequest struct {
	Key        connParticipantKey `json:"key"`
	Candidates []any              `json:"candidates"`
	TTLSeconds int                `json:"ttl_seconds"`
}

type connParticipant struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

type connectivityClient struct {
	url, token string
}

func newConnectivityClient() *connectivityClient {
	token := os.Getenv("CONNECTIVITY_TOKEN")
	if token == "" {
		// The house pattern keeps backend tokens in files, not the
		// environment (NPLN titles mount theirs at /app/connectivity.token).
		if path := os.Getenv("CONNECTIVITY_TOKEN_FILE"); path != "" {
			if raw, err := os.ReadFile(path); err == nil {
				token = string(bytes.TrimSpace(raw))
			} else {
				log.Printf("[spelunky2] connectivity: read token file: %v", err)
			}
		}
	}
	return &connectivityClient{
		url:   os.Getenv("CONNECTIVITY_URL"),
		token: token,
	}
}

func (c *connectivityClient) off() bool { return c.url == "" || c.token == "" }

// register leases one participant generation for an accepted login. It
// returns the lease ID, or "" with the reason logged when the service is
// off or refuses — never an error the login must fail on.
func (c *connectivityClient) register(sessionHex, player string) string {
	if c.off() {
		return ""
	}
	body, err := json.Marshal(connRegisterRequest{
		Key: connParticipantKey{
			Title:      "spelunky2",
			Session:    sessionHex,
			Player:     player,
			Generation: sessionHex,
			Component:  "rendezvous",
		},
		Candidates: []any{},
		TTLSeconds: 300,
	})
	if err != nil {
		return ""
	}
	req, err := http.NewRequest(http.MethodPost, c.url+"/v1/participants", bytes.NewReader(body))
	if err != nil {
		log.Printf("[spelunky2] connectivity: build request: %v", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := connHTTP.Do(req)
	if err != nil {
		log.Printf("[spelunky2] connectivity: unreachable, login continues unleashed")
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("[spelunky2] connectivity: register refused (%s), login continues unleashed", resp.Status)
		return ""
	}
	var p connParticipant
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil || p.ID == "" {
		log.Printf("[spelunky2] connectivity: bad register reply, login continues unleashed")
		return ""
	}
	return p.ID
}

