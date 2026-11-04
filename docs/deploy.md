# Deploy: spelunky2

Plain HTTP on **22660** inside the pod network (health on **22661**); Traefik terminates the
guest's TLS for this name, the same arrangement as [`platforms/switch/games/klei`](../../klei) and
[`servers/t17`](../../t17).

## Running it

```
go run ./cmd/spelunky2d
```

`-addr` (22660), `-health-addr` (22661) and `-edge-addr` (2816/udp, empty
disables) are the knobs. `x.spelunky2.net` reaches it through nx-baas's
`NX_SPELUNKY2_URL` forward (catch-all TLS); the Traefik router + fixed cert
cutover is documented in `docs/protocol.md` §6. Every line it prints before
the next handler lands is a finding.

## Account erasure

`DELETE /internal/accounts/{account_id}/data` on the health listener (`http://openpak-spelunky2:22661`),
`Authorization: Bearer $ACCOUNT_ERASE_KEY` (unset: 503). Also needs `ACCOUNT_GRPC_ADDR` (default
`openpak-account:20001`) and `ACCOUNT_INTERNAL_KEY` to list the account's `switch` link subjects.
Erased: that BAAS user id's `spelunky2_runs`, `spelunky2_tickets`, `spelunky2_sessions` rows and in-memory sessions.
Kept: nothing authored; runs are the player's own so they go too. Not tied to an account (in memory only, token hash
key, gone at restart): the progress blocks of `/get` `/put` and the 2-minute lobby members.

## Results, rankings and rewards (environment)

| Variable | Meaning |
| --- | --- |
| `SPELUNKY2_DATABASE_URL` | Postgres for `legacy-lbs` run results (table `spelunky2_runs`, created at start, pool of 3). Required for `legacy-lbs` and the rankings: unset means both are off. Tickets, session keys and results live only here. |
| `RANKINGS_ADDR` | Rankings contract listener (`:9090` on `openpak.network`, never published). Needs the database. |
| `SPELUNKY2_SCORE_FIELD` | 0, 1 or 2: which of the result's three u64 is the score. Unset: the boards list is empty (no Rankings tab). |
| `REWARDS_EVENTS_URL`, `REWARDS_EVENTS_KEY` | Rewards events; unset disables them. |
| `NX_INTERNAL_URL`, `NX_INTERNAL_KEY` | nx-baas identity (already required). |
