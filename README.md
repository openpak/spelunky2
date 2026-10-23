# spelunky2 — OpenPak's Spelunky 2 backend

The service the Switch build (`01007EC013ABC000`, current update v1.28.0)
reaches when a console is redirected to OpenPak. One process serves the whole
API on one port, dispatching on path:

| Host | What it answers |
| --- | --- |
| `x.spelunky2.net` | `/get` — lobby fetch (join by code), `/put` — lobby publish (host), `/nso` — Nintendo token check, `/64b` — unknown |

Plain HTTP on **22660** inside the pod network (health on **22661**); Traefik terminates the
guest's TLS for this name, the same arrangement as [`platforms/switch/games/klei`](../klei) and
[`servers/t17`](../t17).

`cdn.spelunky2.net` is deliberately **not** redirected: it serves the game's own static
content and it works against Mossmouth.

## Status: /nso implemented, edge echo-reply live, lobby stubs pending

`/nso` verifies the raw BAAS id_token against nx-baas and answers the binary
protocol: the success packet on acceptance, Mossmouth's own 18-byte reject
packet otherwise. Every accepted login also leases one participant generation
from the shared connectivity service (title `spelunky2`, component
`rendezvous`), so players behind one public IP stay distinct when the relay
phase pairs them; unconfigured or failing, the login still succeeds and the
session goes unleashed. Post-login the client sprays 2-byte `{edge_index,
0x14}` UDP probes at the fixed 2816/udp and each is answered with the
hypothesized 20-byte echo reply (`-edge-echo=false` silences it). `/put`,
`/get` and `/64b` still log the redacted request and answer 404 — the first
accepted lobby call decides what they must say. Known gaps are listed in
§2–§4 of [`docs/protocol.md`](docs/protocol.md).

## Why the console's token is not verified here

`/nso` carries a Nintendo token that **OpenPak signed**. This service checks
it against nx-baas's `/internal/switch/identity` instead of phoning Nintendo,
because that answers three things a signature cannot: the player's nickname,
whether the account is banned, and "this player is online now"
(`cmd/spelunky2d/identity.go`).

## The lobby is the host's, not ours

A lobby listing's fields will come from the host's build. Like `kleid`'s
listings, this service will store the object the host wrote and hand it back
unchanged; two builds then agree with each other rather than with us.

## Running it

```
go run ./cmd/spelunky2d
```

`-addr` (22660), `-health-addr` (22661) and `-edge-addr` (2816/udp, empty
disables) are the knobs. `x.spelunky2.net` reaches it through nx-baas's
`NX_SPELUNKY2_URL` forward (catch-all TLS); the Traefik router + fixed cert
cutover is documented in `docs/protocol.md` §6. Every line it prints before
the next handler lands is a finding.
