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
(`cmd/spelunky2d/identity.go`, same contract as `platforms/switch/games/klei`), which also
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

## 4. Edge relay (unmeasured) and shared connectivity

The game links full `nn::socket` TCP+UDP (Bind/Listen/Accept/SendTo/RecvFrom)
and the only gameplay hosts are `edge-%02d.spelunky2.net`. Unknown: how many
edges exist, which one a lobby is assigned (presumably inside the `/put` or
`/get` reply), which ports, TCP vs UDP per flow, and whether the relay is a
dumb packet forwarder or speaks a handshake. The capture must cover
host-then-join across two clients to see both sides of this.

Every accepted login already holds a connectivity lease
(`cmd/spelunky2d/connectivity.go`: participant `spelunky2`/session/player,
component `rendezvous`, TTL 300, renewed by re-posted `/nso`). When a pair
forms, the backend allocates a pair relay for the two lease IDs and binds it
only after the game's own handshake proves each side's mapping toward the
relay endpoint — IP matching alone never binds (openpak/connectivity's rule,
and ours). Open transport questions for that phase: the game dials edge
*names*, and UDP carries no SNI, so answering them needs per-name IPs or
per-edge ports from the `/put`/`/get` capture — undecided until measured.
Needs a `spelunky2` backend token in connectivity's config (live since
2026-10-13: `openpak-spelunky2.token`, config backup beside it) plus
`CONNECTIVITY_URL`/`CONNECTIVITY_TOKEN_FILE` on this service's quadlet;
until then logins go unleashed.

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

## 7. Measured 2026-10-24: the whole online stack

Read out of the v1.28.0 client (disassembly) and checked against Mossmouth's live hosts with
single probe packets. This section supersedes the hypotheses above where they differ.

### Hosts

| Host | Transport | Role |
| --- | --- | --- |
| `x.spelunky2.net` | HTTPS | `/nso`, `/64b`, `/get`, `/put` |
| `edge-NN.spelunky2.net` | UDP 2816 | edge probe (only edge-01..16 exist at Mossmouth) |
| `edge-NN.spelunky2.net` | UDP 29489 | gameplay stream hub |
| `spelunky-v20.mmo.gratis` | UDP 11937 | lobby server |
| `legacy-lbs.mmo.gratis` | UDP 7853 | ranked-run ticket and results |
| `cdn.spelunky2.net/static/<yyyymmdd>` | HTTPS | daily-challenge board (zstd, ~67 MB raw) |

The `.mmo.gratis` names are a second domain; without them in the ceiling the lobby phase goes
to Mossmouth.

### `/nso` reply (18 bytes)

`[0:8]` session key (the client presents it to every later call), `[8:16]` must be non-zero or
the client opens the "Nintendo Switch Online required" applet, `[16]` status (`0xff` =
rejected), `[17]` server protocol version (the client refuses anything above 20).

### Edge probe

`{edge_index, 20}` in, one byte out: the edge index. Only byte 0 is read.

### Lobby server (11937)

Client state packet, sent every 0.5-1 s until it has a session:
`[lobby u64: 0 idle, ~0 create, else join][seq u32][lobby version u32][slot u8][edge RTT x32]`
zero-padded to 0x38, then the player record
`[name len][7 zero][player id u64][option u32][?][platform<<2|ready][character][name]`
(23 + name length bytes).

Reply: `[edge number u64, 1-based][session id u64][lobby id u64][seq echo u32][version u32]
[slot u8][edge RTT echo]` padded to 0x48, then the four records in slot order (one zero byte
for an empty slot). The client ignores a reply whose seq differs from its own and compares
lobby ids without their low five bits. Mossmouth answers an unknown lobby with lobby id 0.

### Edge stream hub (29489)

Client: `[session id 8][bytes held of slot 0..3, 4 x u32][u32 off<<8|len][<=124 bytes]`.
Edge: `[u8 presence<<4 | chunk mask][u24 bytes of the client's stream held]`, then for each
mask bit in slot order `[u32 off<<8|len][bytes]`. 10 s of silence ends the session.

### `legacy-lbs` (7853)

`[NSA id][session key]` (16 bytes, up to five tries) is answered with an 8-byte run ticket;
the run's result is `[NSA id][key][ticket][3 x u64][name 32]`, 20 copies, unanswered.

### `/64b`, `/get`, `/put`

Each body starts with a 64-byte token the client makes once per save from the NSA id, the
session key, a build constant and a clock, chained through a ChaCha-style round function
until a 25-bit proof-of-work condition holds, and then keeps in the save. Only the top half
of the NSA id survives the chaining, so the token itself is the key: our `/64b` code and the
stored progress block are both looked up by it. `/64b` answers a u32 of
five 5-bit indexes into `QWERTYUPASDFGHJKLXCVBMN023456789`; `/get` answers the 0x3620-byte
progress block the client ORs into its save; `/put` sends it.

The token's first four bytes are the low half of the NSA id in the clear. `/64b`, `/get`
and `/put` were not called during a full lobby + play session in Harbor (2026-10-24);
what triggers them is still open.

### Measured 2026-10-24 in Harbor (two consoles)

- "Your Code" on the lobby screen is the lobby id as eight hex digits; "Enter Code" sends
  the typed value as the lobby field of the joiner's state packet.
- Every state packet's record carries the ready flag in byte 21 bit 0 (`0x80` idle,
  `0x81` ready on Switch). The client keeps sending its state (and reads the lobby's
  records from the replies) until a reply carries an edge and a session; then it moves
  to the edge. spelunky2d hands both out once every player in the lobby is ready.
- On the edge the hub's per-slot fan-out (others' streams only, no echo of one's own)
  is enough: both consoles ran the same level in step, each seeing the other's moves,
  for several minutes. A player leaving mid-level leaves the other playing on.
- Daily Challenge: `legacy-lbs` gets the ticket request at the start and the result when
  the run ends. The leaderboard screen still shows Mossmouth's board
  (`cdn.spelunky2.net` is not redirected).

## From the previous README

The service the Switch build (`01007EC013ABC000`, current update v1.28.0)
reaches when a console is redirected to OpenPak. One process serves the whole
API on one port, dispatching on path:

| Host | What it answers |
| --- | --- |
| `x.spelunky2.net` | `/get` — lobby fetch (join by code), `/put` — lobby publish (host), `/nso` — Nintendo token check, `/64b` — unknown |

`cdn.spelunky2.net` is deliberately **not** redirected: it serves the game's own static
content and it works against Mossmouth.
