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

## Results, rankings and rewards (environment)

| Variable | Meaning |
| --- | --- |
| `SPELUNKY2_DATABASE_URL` | Postgres for `legacy-lbs` run results (table `spelunky2_runs`, created at start, pool of 3). Unset: results stay in memory, as before. |
| `RANKINGS_ADDR` | Rankings contract listener (`:9090` on `openpak.network`, never published). Needs the database. |
| `SPELUNKY2_SCORE_FIELD` | 0, 1 or 2: which of the result's three u64 is the score. Unset: the boards list is empty (no Rankings tab). |
| `REWARDS_EVENTS_URL`, `REWARDS_EVENTS_KEY` | Rewards events; unset disables them. |
| `NX_INTERNAL_URL`, `NX_INTERNAL_KEY` | nx-baas identity (already required). |
