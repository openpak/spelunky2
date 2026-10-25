# Deploy: spelunky2

Plain HTTP on **22660** inside the pod network (health on **22661**); Traefik terminates the
guest's TLS for this name, the same arrangement as [`platforms/switch/games/klei`](../../klei) and
[`servers/t17`](../../t17).

## Running it

```
go run ./cmd/spelunky2d
```

`-addr` (22660), `-health-addr` (22661) and `-edge-addr` (2816/udp, empty
disables) are the knobs. `x.spelunky2.net` reaches it through nx-baas's
`NX_SPELUNKY2_URL` forward (catch-all TLS); the Traefik router + fixed cert
cutover is documented in `docs/protocol.md` §6. Every line it prints before
the next handler lands is a finding.
