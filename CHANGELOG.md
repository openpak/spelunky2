# Changelog — spelunky2

## v0.6.0 — 2026-10-13

**Dumb relay between gameplay senders.** The 152-byte session packets two
clients spray at the session port are now forwarded to the other recent
sender(s), pairing by recency until `/put` names the lobby. If the edge is a
relay (not a server), movements should start syncing with no further
protocol work; if the client wants server answers instead, the unchanged
stall names the next gap. Pairing is exact for two players.

Multi-port listen and whole-packet logging: `-edge-addr` takes a comma
list (2816 discovery + 29489 session port so far); longer gameplay-phase
packets are logged whole (128-byte cap) for the handshake RE instead of
just their heads.

## v0.5.0 — 2026-10-13

**Echo-reply to edge probes (hypothesis).** The 2-byte `{edge_index, 0x14}`
probe asks with magic 20, and the client's receive path selects the edge
entry by a reply byte & 0x1f — so each probe is answered with its own two
bytes inside a 20-byte reply, zeros elsewhere. If the client advances
(`/put`, gameplay handshake, or a new error), the shape is confirmed or the
next gap names itself. `-edge-echo=false` restores silence for comparison.

**Edge probe listener.** After a successful `/nso` the client resolves
edge-00 through edge-31 and sprays 2-byte UDP probes (`{edge_index, 0x24}`,
8 rounds of 32) at its fixed port 2816/udp, picking by reply latency
(measured live with strace: all 256 to one address, nothing to a second).
`spelunky2d` now listens on 2816/udp (`-edge-addr`, empty disables) and logs
every probe redacted, answering nothing until the reply shape is known.
Needs 2816/udp published on the quadlet, an OCI ingress rule and a host
ACCEPT — without those the probes die upstream (measured: box tcpdump empty
while the client sent 256).

Token from a file: `CONNECTIVITY_TOKEN_FILE` is read when
`CONNECTIVITY_TOKEN` is empty — the house pattern (NPLN titles mount theirs
at `/app/connectivity.token`). Live with the `spelunky2` backend token since
this version.

## v0.3.0 — 2026-10-13

**Shared-connectivity leases.** Every accepted `/nso` registers one
participant generation (`spelunky2` / session / verified player /
`rendezvous`) with servers/connectivity, so the relay phase has lease IDs to
name when a host/joiner pair forms — two players behind one NAT stay two
participants. Env-gated (`CONNECTIVITY_URL` + `CONNECTIVITY_TOKEN`), stdlib
only, and never fatal: unconfigured or refused, the login succeeds unleashed
and the line says so. Pair relays and edge transport stay unbuilt until
`/put` and `/get` show the lobby shape.

**`/nso` verifies and answers.** The first live capture showed the game
posting its raw BAAS id_token as the whole body and the real server speaking
binary (18-byte reject packet, measured). This version verifies the token
against nx-baas `/internal/switch/identity`, mints a remembered 16-byte
session per token, and answers the hypothesized success packet
(`session[16]` + status zero) — or Mossmouth's own reject packet,
byte-for-byte, on any failure. The client's next request settles whether the
hypothesis holds. Reached via nx-baas's new `NX_SPELUNKY2_URL` forward until
the Traefik cutover.

## v0.1.0 — 2026-10-13

**Capture stubs for Spelunky 2's own backend.** Static scan of the Switch
v1.28.0 main (SDK 13.3.0) confirms a bespoke Mossmouth/BlitWorks stack —
`x.spelunky2.net` HTTPS rendezvous plus `edge-%02d.spelunky2.net` raw-socket
relays — with no NEX, NPLN, Photon, EOS or PlayFab anywhere. `spelunky2d`
serves all four API paths (`/get`, `/put`, `/nso`, `/64b`) as logging stubs:
every request is logged with credentials, codes and names redacted, and
answered 404, so the first Harbor boot writes the protocol spec the real
handlers get built against. Identity lookup against nx-baas is implemented and
unwired pending the `/nso` field name. Ports 22660–22669 claimed. Nothing
console-verified yet; deploy checklist (ceiling, Traefik, quadlet, catalog)
in `docs/protocol.md` §6.
