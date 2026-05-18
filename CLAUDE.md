# CLAUDE.md

Guidance for working in this repository.

## What this is

`rescuestream-dji-bridge` is a Go service that bridges a DJI Matrice 30T flown on
a DJI RC Plus to a self-hosted cloud platform over the DJI **pilot-to-cloud Cloud
API (MQTT 5.0)**.

- **Spec:** `m30t-rcplus-cloud-mvp.md` is the project design doc; code comments
  cite its sections (e.g. "§6.2"). It is a *derived summary* — the authoritative
  wire protocol (topic strings, message schemas, OSD field names) is the DJI
  `dji-sdk/Cloud-API-Doc` repo. Verify protocol details against the real docs,
  never the summary alone.
- **MVP phases:** P0 onboarding/connect, P1 telemetry ingestion, P2 live
  streaming, P3 DRC payload control. P0+P1 are implemented; P2/P3 are stubs.

## Layout

- `cmd/rescuestream-dji-bridge/` — backend entrypoint
- `cmd/mock-rcplus/` — RC Plus simulator for testing without DJI hardware
- `internal/config/`, `internal/logging/` — app config and logging helpers
- `internal/mqtt/` — autopaho MQTT 5.0 client wrapper
- `internal/cloudapi/` — DJI Cloud API protocol layer: `message` (envelope),
  `topic` (parse/build + router), `correlation` (tid request/reply),
  `telemetry`, `live` (P2 stub), `drc` (P3 stub)
- `internal/device/` — device registry + in-memory state store
- `internal/onboarding/` — HTTP server, JSBridge page, JWT credential minting

## Conventions

- **Functional options for client/server/service constructors.** Any type that
  owns a connection or long-lived resource (MQTT clients, the HTTP server,
  command services) is built as `New(required..., ...Option)` where
  `Option func(*T)` and each option is a `With*` function. Required parameters
  are positional; everything optional is a `With*` option with a sane default
  applied before options run. Reference implementation: `internal/mqtt`.
- **Lenient JSON decoding.** Never hard-fail on unknown fields — DJI adds fields
  across firmware versions (§10). Decode into typed structs and ignore the rest.
- **MQTT via autopaho, not bare paho.** autopaho owns reconnection and
  re-subscription; (re-)subscribe inside `OnConnectionUp`.
- **Device auth is per-device JWT (RS256).** The onboarding endpoint mints
  short-lived tokens; EMQX verifies them with the public key.
- **Errors wrap with context:** `fmt.Errorf("doing thing: %w", err)` — lowercase,
  no trailing punctuation.
- **Structured logging via `log/slog`** (JSON handler). Pass a `*slog.Logger`
  into constructors; libraries should not log to the global default.
- **App config via `caarlos0/env`** struct tags in `internal/config` — env vars
  only, no flags or config files.
- **HTTP endpoints are API-key gated.** When `API_KEY` is set, every endpoint
  requires it (`X-API-Key` header, `Authorization: Bearer`, or `?key=`);
  `/api/onboard` also enforces `SN_ALLOWLIST`. An empty `API_KEY` runs open and
  is development-only (logged as a warning at startup).

## Build & test

- `go build ./...`, `go vet ./...`, `go test ./...`
- `docker-compose up` — backend + EMQX broker + Grafana LGTM stack
- `go run ./cmd/mock-rcplus` — exercise the bridge without DJI hardware
- CI runs golangci-lint, yamllint, hadolint, and commitlint; commits must follow
  Conventional Commits.
