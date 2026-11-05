package main

// Persistence of the legacy-lbs run results and the rankings contract over them.
//
// Without SPELUNKY2_DATABASE_URL the service behaves as before: results are logged and
// held in memory only. The three u64 numbers in a result are stored raw; which of them is
// the score has not been measured (docs/protocol.md), so the boards stay empty until
// SPELUNKY2_SCORE_FIELD (0, 1 or 2) names it.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openpak/rewards-emit"

	"openpak/spelunky2/internal/rankingsapi"
)

// storedRun is one result as persisted.
type storedRun struct {
	Ticket  uint64
	Player  string // BAAS user id of the session that sent it
	Values  [3]uint64
	Created time.Time
}

// standing is a player's best on a board.
type standing struct {
	Player  string
	Score   int64
	Updated time.Time
}

type runStore interface {
	// SaveSession remembers which player a session key (first 8 session bytes) belongs to.
	SaveSession(ctx context.Context, key [8]byte, player string) error
	// SessionPlayer is the player a session key was saved for.
	SessionPlayer(ctx context.Context, key [8]byte) (string, bool, error)
	// IssueTicket records a ticket handed to a player.
	IssueTicket(ctx context.Context, ticket uint64, player string, at time.Time) error
	// Insert stores the run; false when its ticket was not issued to r.Player or the run is
	// already stored (the 20 copies).
	Insert(ctx context.Context, r storedRun) (bool, error)
	// Standings is every player's best value of the given field, best first. day is a
	// UTC "2006-01-02" filter, empty for all time.
	Standings(ctx context.Context, day string, field int) ([]standing, error)
}

type pgStore struct{ pool *pgxpool.Pool }

var fieldCols = [3]string{"v0", "v1", "v2"}

func openStore(ctx context.Context, url string) (*pgStore, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 3
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS spelunky2_sessions (
  key          bytea PRIMARY KEY,
  baas_user_id text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS spelunky2_tickets (
  ticket       bigint PRIMARY KEY,
  baas_user_id text NOT NULL,
  issued_at    timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS spelunky2_runs (
  ticket     bigint PRIMARY KEY,
  baas_user_id text NOT NULL,
  v0 bigint NOT NULL, v1 bigint NOT NULL, v2 bigint NOT NULL,
  day        date NOT NULL,
  created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS spelunky2_runs_day ON spelunky2_runs (day);
-- 2026-11-05 (prds#131): the name the client sent was never read; the account core owns names.
ALTER TABLE spelunky2_runs DROP COLUMN IF EXISTS name;`)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &pgStore{pool}, nil
}

func (s *pgStore) SaveSession(ctx context.Context, key [8]byte, player string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO spelunky2_sessions (key, baas_user_id) VALUES ($1,$2)
ON CONFLICT (key) DO UPDATE SET baas_user_id = EXCLUDED.baas_user_id`, key[:], player)
	return err
}

func (s *pgStore) SessionPlayer(ctx context.Context, key [8]byte) (string, bool, error) {
	var p string
	err := s.pool.QueryRow(ctx, `SELECT baas_user_id FROM spelunky2_sessions WHERE key=$1`, key[:]).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return p, err == nil, err
}

func (s *pgStore) IssueTicket(ctx context.Context, ticket uint64, player string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO spelunky2_tickets (ticket, baas_user_id, issued_at) VALUES ($1,$2,$3)`,
		int64(ticket), player, at)
	return err
}

func (s *pgStore) Insert(ctx context.Context, r storedRun) (bool, error) {
	t, err := s.pool.Exec(ctx, `INSERT INTO spelunky2_runs (ticket, baas_user_id, v0, v1, v2, day, created_at)
SELECT ticket, baas_user_id, $3, $4, $5, $6, $7 FROM spelunky2_tickets WHERE ticket=$1 AND baas_user_id=$2
ON CONFLICT (ticket) DO NOTHING`,
		int64(r.Ticket), r.Player, int64(r.Values[0]), int64(r.Values[1]), int64(r.Values[2]),
		r.Created.UTC().Format("2006-01-02"), r.Created)
	return err == nil && t.RowsAffected() == 1, err
}

func (s *pgStore) Standings(ctx context.Context, day string, field int) ([]standing, error) {
	col := fieldCols[field]
	q := `SELECT baas_user_id, MAX(` + col + `), MAX(created_at) FROM spelunky2_runs`
	args := []any{}
	if day != "" {
		q += ` WHERE day = $1`
		args = append(args, day)
	}
	q += ` GROUP BY baas_user_id ORDER BY 2 DESC, 3, 1`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []standing
	for rows.Next() {
		var st standing
		if err := rows.Scan(&st.Player, &st.Score, &st.Updated); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// scoreField parses SPELUNKY2_SCORE_FIELD; -1 when unset or invalid.
func scoreField() int {
	n, err := strconv.Atoi(os.Getenv("SPELUNKY2_SCORE_FIELD"))
	if err != nil || n < 0 || n > 2 {
		return -1
	}
	return n
}

// rankSource serves the contract over a runStore. now is the clock (tests).
type rankSource struct {
	st    runStore
	field int
	now   func() time.Time
}

func (r rankSource) Boards(context.Context) ([]rankingsapi.Board, error) {
	if r.field < 0 {
		return nil, nil
	}
	day := r.now().UTC().Format("2006-01-02")
	return []rankingsapi.Board{
		{ID: "daily", Name: "Daily Challenge", Kind: rankingsapi.KindScore, Order: rankingsapi.OrderDesc, Unit: "points", Season: day},
		{ID: "best", Name: "Best runs", Kind: rankingsapi.KindScore, Order: rankingsapi.OrderDesc, Unit: "points"},
	}, nil
}

func (r rankSource) Rows(ctx context.Context, board string, q rankingsapi.Query) (rankingsapi.Page, error) {
	day := ""
	switch {
	case r.field < 0:
		return rankingsapi.Page{}, rankingsapi.ErrNoBoard
	case board == "daily":
		day = r.now().UTC().Format("2006-01-02")
	case board != "best":
		return rankingsapi.Page{}, rankingsapi.ErrNoBoard
	}
	sts, err := r.st.Standings(ctx, day, r.field)
	if err != nil {
		return rankingsapi.Page{}, fmt.Errorf("standings: %w", err)
	}
	sort.SliceStable(sts, func(i, j int) bool { return sts[i].Score > sts[j].Score })
	all := make([]rankingsapi.Row, len(sts))
	for i, s := range sts {
		all[i] = rankingsapi.Row{PID: s.Player, Score: s.Score, UpdatedAt: s.Updated}
	}
	return rankingsapi.Window(rankingsapi.KindScore, all, q), nil
}

// openStoreFromEnv returns the database store, or nil with the reason logged when
// SPELUNKY2_DATABASE_URL is unset or the database cannot be reached.
func openStoreFromEnv() *pgStore {
	url := os.Getenv("SPELUNKY2_DATABASE_URL")
	if url == "" {
		log.Printf("[spelunky2] SPELUNKY2_DATABASE_URL unset: legacy-lbs (run tickets and results) is off")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := openStore(ctx, url)
	if err != nil {
		log.Printf("[spelunky2] database unavailable: %v", err)
		return nil
	}
	return st
}

// startLBS brings up the contract server and legacy-lbs once the database is reachable,
// retrying every 15 s. Nothing is ever answered from memory.
func startLBS(addr string, sess *sessions) {
	if os.Getenv("SPELUNKY2_DATABASE_URL") == "" {
		openStoreFromEnv()
		return
	}
	go func() {
		var st *pgStore
		for st = openStoreFromEnv(); st == nil; st = openStoreFromEnv() {
			time.Sleep(15 * time.Second)
		}
		log.Printf("[spelunky2] results stored in postgres")
		rankingsapi.Serve(os.Getenv("RANKINGS_ADDR"), "switch", rankSource{st, scoreField(), time.Now})
		sess.attach(st)
		l := newLBS(sess, st)
		l.rw = emit.FromEnv("spelunky2")
		serveLBS(addr, l)
	}()
}
