# Notes: spelunky2

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
§2–§4 of [`protocol.md`](protocol.md).

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
