# syntax=docker/dockerfile:1
# Spelunky 2's own online stack (lobby rendezvous + Nintendo token check) for the Switch build.
# Plain HTTP inside the pod network — Traefik terminates the guest's TLS for the
# x.spelunky2.net name, the same arrangement as platforms/switch/games/klei, servers/t17 and
# the Among Us front.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/spelunky2d ./cmd/spelunky2d

FROM gcr.io/distroless/static
COPY --from=build /out/spelunky2d /spelunky2d
USER nonroot:nonroot
EXPOSE 22660 22661
ENTRYPOINT ["/spelunky2d"]
