# Ports

Claimed from the third-party-network range (`22600–22699`, one block of ten per network), after
Among Us (22600–22609), `servers/demonware` (22610–22619), `servers/eos` (22620–22629),
`servers/t17` (22630–22639), `platforms/switch/games/klei` (22640–22649) and `servers/playfab` (22650–22659).

| Port | Use |
| --- | --- |
| 22660 | HTTP for `x.spelunky2.net` (behind Traefik, which terminates the guest's TLS) |
| 22661 | `/health` |
| 22662–22669 | reserved for this network |
| 2816/udp | edge-probe listener (the game's fixed port, outside every plan range — see below) |

One port serves all four API paths: they are distinguished by path, not by
port, and Traefik routes the one name to this service. The edge relays
(`edge-00` … `edge-31.spelunky2.net`) are a separate transport (raw UDP
sockets dialled by hostname) and cannot ride Traefik: there is no SNI in UDP.

2816/udp is fixed by the game (measured live: 2-byte `{edge_index, 0x24}`
probes, 8 rounds of 32), so it sits outside the plan like nncs's 22360 or
Demonware's 3074: dialled directly, published on the host, with a provider
ingress rule and a host ACCEPT. All 32 names resolve to the one address today;
per-edge answers come later with the relay design (protocol.md §4).
