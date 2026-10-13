# spelunky2 — OpenPak's Spelunky 2 backend

The service the Switch build (`01007EC013ABC000`, current update v1.28.0)
reaches when a console is redirected to OpenPak. One process serves the whole
API on one port, dispatching on path:

| Host | What it answers |
| --- | --- |
| `x.spelunky2.net` | `/get` — lobby fetch (join by code), `/put` — lobby publish (host), `/nso` — Nintendo token check, `/64b` — unknown |

Plain HTTP on **22660** inside the pod network (health on **22661**); Traefik terminates the
guest's TLS for this name, the same arrangement as [`servers/klei`](../klei) and
[`servers/t17`](../t17).

`cdn.spelunky2.net` is deliberately **not** redirected: it serves the game's own static
content and it works against Mossmouth.

## Status: stubs built, nothing console-verified

All four endpoints log the redacted request and answer 404. The request and
reply shapes are still unknown — static strings give the paths but not the
fields — so the first Harbor boot is the implementation: its log lines are the
spec the handlers get written against. Known gaps are listed in §2–§4 of
[`docs/protocol.md`](docs/protocol.md); the nearest one is which `/nso` field
carries the Nintendo token. The gameplay relay (`edge-%02d.spelunky2.net`,
raw TCP+UDP) is unmeasured and untouched by this process.

## Why the console's token is not verified here

`/nso` carries a Nintendo token that **OpenPak signed**. This service will check
it against nx-baas's `/internal/switch/identity` instead of phoning Nintendo,
because that answers three things a signature cannot: the player's nickname,
whether the account is banned, and "this player is online now". The lookup is
implemented (`cmd/spelunky2d/identity.go`) and unwired until the capture names
the field.

## The lobby is the host's, not ours

A lobby listing's fields will come from the host's build. Like `kleid`'s
listings, this service will store the object the host wrote and hand it back
unchanged; two builds then agree with each other rather than with us.

## Running it

```
go run ./cmd/spelunky2d
```

`-addr` (22660) and `-health-addr` (22661) are the knobs. Point a Harbor boot
at it (ceiling + Traefik per `docs/protocol.md` §6) and read the log: every
line it prints before the first real handler lands is a finding.
