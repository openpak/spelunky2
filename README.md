# Spelunky 2

OpenPak's Spelunky 2 Server.

| | |
| --- | --- |
| Game | [Spelunky 2](https://openpak.org/titles#t-141) |
| Network | Nintendo Switch |
| Title ID | `01007EC013ABC000` |
| Image | `ghcr.io/openpak/spelunky2` |
| Licence | AGPL-3.0-only |

## What it serves

| Host | What it answers |
| --- | --- |
| `x.spelunky2.net` | `/get` — lobby fetch (join by code), `/put` — lobby publish (host), `/nso` — Nintendo token check, `/64b` — unknown |

## Run

```sh
go run ./cmd/spelunky2d
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-addr` | `:22660` | HTTP listener (Traefik terminates TLS in front) |
| `-health-addr` | `:22661` | Health listener |
| `-edge-addr` | `:2816` | Edge probe UDP listeners, comma-separated (empty disables) |

## Docs

- [Protocol](docs/protocol.md)
- [Evidence](docs/evidence.md)
- [Deploy](docs/deploy.md)
- [Notes](docs/notes.md)
- [Ports](docs/ports.md)

Live compatibility status: [openpak.org/titles#t-141](https://openpak.org/titles#t-141).

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
