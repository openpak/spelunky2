# Spelunky 2 wire protocol (Switch, v1.28.0)

Recovered 2026-10-13 from static strings in the decompressed update main
(`01007EC013ABC000`, SDK 13.3.0; base v0 carries the same endpoints plus a
now-gone `cosmic-ocean.spelunky2.net`). No NEX, NPLN, Photon, EOS or PlayFab
markers anywhere — this is Mossmouth/BlitWorks' own HTTPS + raw sockets.
Evidence and method: `scratch/spelunky2/NOTES.md`.

## 1. Hosts

| Host | Serves | Redirect? |
| --- | --- | --- |
| `x.spelunky2.net` | the whole API below (`/get`, `/put`, `/nso`, `/64b`) | yes — this service |
| `edge-%02d.spelunky2.net` | numbered gameplay relays | yes, once measured (§4) |
| `cdn.spelunky2.net` | static content (`/static/…`) | **no** — the game's own CDN, works against Mossmouth |
| `cosmic-ocean.spelunky2.net` | base v0 only, gone in v1.28.0 | no |

TLS: stock libcurl (`NintendoSDK_libcurl-13_3_0`); no embedded CA bundle, no
SHA256 pins, no custom verify strings found. The OpenPak path is Traefik
serving an OpenPak-CA leaf — unproven until a boot.

## 2. Endpoints (shapes unknown — the Harbor capture fills these in)

| Method | Path | From the strings | Gap |
| --- | --- | --- | --- |
| ? | `https://x.spelunky2.net/get` | lobby fetch; UI reads "Enter Code" / "Error joining game" | field carrying the code (query? body?); reply shape (edge host? session blob?) |
| ? | `https://x.spelunky2.net/put` | lobby publish (host) | body shape (code? edge choice? player info?); keepalive or one-shot? |
| ? | `https://x.spelunky2.net/nso` | Nintendo token check — the game reads the `nn::account` token cache and the binary holds no `*.nintendo.*` URL, so verification is server-side | which token (NSA id_token?), which field, what reply gates the menu |
| ? | `https://x.spelunky2.net/64b` | unknown | everything — may be version/config rather than lobby |

Even the HTTP methods are unconfirmed: `/get` and `/put` are path names, not
verbs. The stubs log method + redacted URL + redacted body for every hit, so
the first boot writes this table.

## 3. Identity

`/nso` carries a Nintendo token OpenPak itself signed. This service will not
phone Nintendo about it — it asks nx-baas `POST /internal/switch/identity`
(`cmd/spelunky2d/identity.go`, same contract as `servers/klei`), which also
answers nickname, banned state and online presence. Wired up once the capture
names the field.

## 4. Edge relay (unmeasured)

The game links full `nn::socket` TCP+UDP (Bind/Listen/Accept/SendTo/RecvFrom)
and the only gameplay hosts are `edge-%02d.spelunky2.net`. Unknown: how many
edges exist, which one a lobby is assigned (presumably inside the `/put` or
`/get` reply), which ports, TCP vs UDP per flow, and whether the relay is a
dumb packet forwarder or speaks a handshake. The capture must cover
host-then-join across two clients to see both sides of this.

## 5. What is deliberately not served

- `cdn.spelunky2.net` — static game content; works against Mossmouth.
- `cosmic-ocean.spelunky2.net` — dead in the current update; not redirected
  unless a boot asks for it.

## 6. Deploy checklist (owner-side, not in this repo)

1. `openpak-policy ceiling add switch .spelunky2.net` → scp the envelope to
   the box (`docs/signed-ceiling.md`); add the family to
   `netprofile-platforms.yml`.
2. Traefik TCP router for `x.spelunky2.net` (SNI) → `spelunky2d:22660`;
   edge names later per §4.
3. Quadlet/container for `ghcr.io/openpak/spelunky2` with `NX_INTERNAL_URL`
   + `NX_INTERNAL_KEY`; publish 22661 for `/health` (internal).
4. Update the website catalog row (Spelunky 2, `01007EC013ABC000`): backend
   `spelunky2.net`, notes with the live status.
