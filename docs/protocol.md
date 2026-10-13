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

## 2. Endpoints (measured 2026-10-13, Harbor + box capture)

The transport is **binary**: `application/octet-stream` replies, HTTP/2
(ALPN `h2 http/1.1`), on-demand OpenPak-CA cert accepted by the game.

| Exchange | Request | Real-server reply |
| --- | --- | --- |
| `POST /nso` | the **raw BAAS id_token as the whole body** (1269 B, `Content-Type: application/x-www-form-urlencoded`, `Accept: */*`, no field names at all) | invalid token: 200, 18 bytes `00×16 + ff 00` — the reject packet. Hypothesis: `{session[16], status u16le}` with status `0x00ff` = rejected; success presumably fills the session and returns `0x0000`. Unproven — needs a valid token |
| `GET /get`, `POST /get` (guessed `?code=` / `code=` fields) | — | 200, empty (guessed shapes; the real join carries a session from `/nso`) |
| `GET /put`, `POST /put` (guessed `code=`) | — | 200, empty |
| `GET /64b`, `GET /` | — | 200, empty |
| `POST /64b` (`code=ABCD`) | — | 200, **4 bytes `ba 78 e6 01`** (u32le `0x01e678ba`; meaning unknown — version? error? recorded, not interpreted) |

The `/nso` token is the standard BAAS id_token (kid `openpak-baas-key-1`,
per-console `iss`, `aud ed9e2f05d286f7b8`, ~3 h validity). Claims that matter
to us: `nintendo.ai` (title id), `nintendo.av` (`00080000` = update v1.28.0 —
the server can gate versions), `sub` (player), plus an embedded `openpak` ticket.
Full capture method (one-shot `NXBAAS_TRACE_BODIES=1`, reverted immediately)
is recorded in `scratch/spelunky2/NOTES.md`.

Client flow measured: enter online → single `POST /nso` → on 404 the game
opens the **error applet** (`0100000000001005`). No `/put`, `/get`, edge or CDN
calls follow a rejection.

## 3. Identity

`/nso` carries the raw BAAS id_token as its whole body. This service verifies
it against nx-baas `POST /internal/switch/identity`
(`cmd/spelunky2d/identity.go`, same contract as `servers/klei`), which also
answers nickname, banned state and online presence. On success the service
must return the success packet (§2 hypothesis); on rejection it returns the
18-byte reject packet byte-for-byte.

## 3a. Reaching the service: forward via nx-baas

`spelunky2d` is plain HTTP and no fixed leaf can be minted here (the CA key is
owner-held), so Traefik cannot terminate for it directly. Until the cutover,
nx-baas forwards the name like it forwards PlayFab (`NX_PLAYFAB_URL`
precedent): an env-gated branch (`NX_SPELUNKY2_URL`, default off) that
reverse-proxies `x.spelunky2.net` to the container and logs redacted. The
Traefik router + fixed cert + direct profile claim stay the end state.

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
