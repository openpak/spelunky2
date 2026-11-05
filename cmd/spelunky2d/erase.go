package main

// Account erasure (openpak/prds#120). DELETE /internal/accounts/{account_id}/data with
// `Authorization: Bearer <ACCOUNT_ERASE_KEY>` removes everything this service holds for the
// OpenPak account. The service keys its players by the BAAS user id (the account's "switch" link
// subject), so the core's links are asked for the subject (player.BaasUserID).
// The endpoint is mounted on the health listener (container network only). Unset key: 503.

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	accountv1 "github.com/openpak/account-api/openpak/account/v1"
)

const eraseService = "spelunky2"

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// linkLister answers which console ids the core still holds for an account.
type linkLister interface {
	SwitchSubjects(ctx context.Context, accountID string) ([]string, error)
}

type coreLinks struct{ links accountv1.LinksClient }

func newCoreLinks(addr, key string) (*coreLinks, error) {
	if len(key) < 32 {
		return nil, errors.New("ACCOUNT_INTERNAL_KEY must be set (>=32 bytes)")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any,
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(metadata.AppendToOutgoingContext(ctx, "x-internal-key", key), method, req, reply, cc, opts...)
		}))
	if err != nil {
		return nil, err
	}
	return &coreLinks{links: accountv1.NewLinksClient(conn)}, nil
}

func (c *coreLinks) SwitchSubjects(ctx context.Context, accountID string) ([]string, error) {
	resp, err := c.links.ListAccountLinks(ctx, &accountv1.ListAccountLinksRequest{AccountId: accountID, Namespace: "switch"})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range resp.GetLinks() {
		if l.GetNamespace() == "switch" && l.GetSubjectId() != "" {
			out = append(out, l.GetSubjectId())
		}
	}
	return out, nil
}

// Linked reports whether an active OpenPak account owns the BAAS user id (link at first use, prds#131).
func (c *coreLinks) Linked(ctx context.Context, baas string) (bool, error) {
	l, err := c.links.GetLinkBySubject(ctx, &accountv1.GetLinkBySubjectRequest{Namespace: "switch", SubjectId: strings.ToLower(baas)})
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return l.GetState() == accountv1.LinkState_LINK_STATE_ACTIVE && l.GetAccountId() != "", nil
}

// eraseStore is the durable side: spelunky2_sessions, _tickets and _runs, all keyed by BAAS user id.
type eraseStore interface {
	DeleteAccount(ctx context.Context, players []string) (int, error)
}

// DeleteAccount removes the players' session keys, issued tickets and stored runs in one transaction.
func (s *pgStore) DeleteAccount(ctx context.Context, players []string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	n := 0
	for _, t := range []string{"spelunky2_runs", "spelunky2_tickets", "spelunky2_sessions"} {
		tag, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE baas_user_id = ANY($1)`, players)
		if err != nil {
			return 0, err
		}
		n += int(tag.RowsAffected())
	}
	return n, tx.Commit(ctx)
}

// forgetPlayers drops the in-memory sessions minted for the players and returns how many.
func (st *sessions) forgetPlayers(players map[string]bool) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	gone := map[string]bool{}
	for k, p := range st.byKey {
		if players[p.BaasUserID] {
			delete(st.byKey, k)
			n++
		}
	}
	for id, p := range st.bySessID {
		if players[p.BaasUserID] {
			delete(st.bySessID, id)
			delete(st.leases, hex.EncodeToString([]byte(id)))
			gone[id] = true
		}
	}
	for tok, sess := range st.byToken {
		if gone[string(sess[:])] {
			delete(st.byToken, tok)
		}
	}
	return n
}

// Erase removes every row and session of the account. Idempotent.
func (s *server) Erase(ctx context.Context, accountID string) (int, error) {
	if s.links == nil {
		return 0, errors.New("account core not configured")
	}
	subs, err := s.links.SwitchSubjects(ctx, accountID)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		log.Printf("account erased account=%s rows=%d service=%s", accountID, 0, eraseService)
		return 0, nil
	}
	set := map[string]bool{}
	for _, sub := range subs {
		set[sub] = true
	}
	rows := 0
	s.sessions.mu.Lock()
	st := s.sessions.store
	s.sessions.mu.Unlock()
	if db, ok := st.(eraseStore); ok {
		n, err := db.DeleteAccount(ctx, subs)
		if err != nil {
			return 0, err
		}
		rows += n
	}
	rows += s.sessions.forgetPlayers(set)
	log.Printf("account erased account=%s rows=%d service=%s", accountID, rows, eraseService)
	return rows, nil
}

// eraseHandler serves DELETE /internal/accounts/{account_id}/data.
func (s *server) eraseHandler(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if key == "" {
			writeJSON(w, 503, map[string]any{"error": "erase not configured"})
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == r.Header.Get("Authorization") || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeJSON(w, 401, map[string]any{"error": "unauthorized"})
			return
		}
		id := r.PathValue("account_id")
		if !uuidRe.MatchString(id) {
			writeJSON(w, 400, map[string]any{"error": "account_id must be a UUID"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		rows, err := s.Erase(ctx, strings.ToLower(id))
		if err != nil {
			log.Printf("[spelunky2] erase failed: %v", err)
			writeJSON(w, 500, map[string]any{"error": "erase failed"})
			return
		}
		writeJSON(w, 200, map[string]any{"service": eraseService, "rows": rows})
	}
}

// setupErase wires the core client from the environment and mounts the endpoint on mux.
func (s *server) setupErase(mux *http.ServeMux) {
	key := os.Getenv("ACCOUNT_ERASE_KEY")
	addr := os.Getenv("ACCOUNT_GRPC_ADDR")
	if addr == "" {
		addr = "openpak-account:20001"
	}
	if key != "" {
		l, err := newCoreLinks(addr, os.Getenv("ACCOUNT_INTERNAL_KEY"))
		if err != nil {
			log.Printf("[spelunky2] ERROR erase endpoint cannot reach the account core: %v", err)
		} else {
			s.links = l
			s.linked = newLinkGate(l, 5*time.Minute)
		}
	}
	mux.HandleFunc("DELETE /internal/accounts/{account_id}/data", s.eraseHandler(key))
}
