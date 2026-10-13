# Changelog — spelunky2

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
