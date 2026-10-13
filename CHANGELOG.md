# Changelog — spelunky2

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
