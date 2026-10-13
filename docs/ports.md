# Ports

Claimed from the third-party-network range (`22600–22699`, one block of ten per network), after
Among Us (22600–22609), `servers/demonware` (22610–22619), `servers/eos` (22620–22629),
`servers/t17` (22630–22639), `servers/klei` (22640–22649) and `servers/playfab` (22650–22659).

| Port | Use |
| --- | --- |
| 22660 | HTTP for `x.spelunky2.net` (behind Traefik, which terminates the guest's TLS) |
| 22661 | `/health` |
| 22662–22669 | reserved for this network (the edge relay, once the capture shows what it needs) |

One port serves all four API paths: they are distinguished by path, not by port, and Traefik
routes the one name to this service. The edge relays (`edge-%02d.spelunky2.net`) are a
separate transport (raw TCP+UDP sockets) and will claim reserved ports here once measured —
see `protocol.md` §4.
