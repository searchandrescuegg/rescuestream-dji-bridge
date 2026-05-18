# rescuestream-dji-bridge

A Go service that bridges a DJI Matrice 30T flown on a DJI RC Plus to a
self-hosted cloud platform over the DJI **pilot-to-cloud Cloud API (MQTT 5.0)**.

`m30t-rcplus-cloud-mvp.md` is the design document; the authoritative wire
protocol is the DJI [`Cloud-API-Doc`](https://github.com/dji-sdk/Cloud-API-Doc)
repository.

## What it does

- **Onboarding** — serves the JSBridge page DJI Pilot 2 loads in its WebView and
  issues per-device, short-lived RS256 JWT credentials for the broker.
- **Topology** — learns the RC Plus ⇄ M30T pairing from `update_topo`.
- **Telemetry** — ingests live M30T OSD/state, exposed as JSON and an SSE stream.
- **Live streaming & DRC** — P2/P3 scaffolding is in place (`cloudapi/live`,
  `cloudapi/drc`); full behavior is future work.

MVP phases: **P0** onboarding/connect and **P1** telemetry are implemented;
**P2** live streaming and **P3** DRC payload control are stubs.

## HTTP endpoints

Every endpoint is gated by `API_KEY` when one is configured (`X-API-Key` header,
`Authorization: Bearer`, or `?key=` query parameter).

| Endpoint | Purpose |
|---|---|
| `GET /` | JSBridge onboarding page (loaded inside DJI Pilot 2) |
| `POST /api/onboard` | Issue MQTT broker host + per-device credentials |
| `GET /api/devices` | Latest telemetry snapshot for every device |
| `GET /api/devices/stream` | Server-Sent Events stream of telemetry updates |

## Configuration

Configuration is environment-variable based ([env](https://github.com/caarlos0/env)):

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Logging level (debug, info, warn, error) | `info` |
| `HTTP_PORT` | Onboarding/telemetry HTTP port | `8080` |
| `API_KEY` | API key required on every HTTP endpoint; empty = open (dev only) | - |
| `SN_ALLOWLIST` | Comma-separated RC Plus serials allowed to onboard; empty = any | - |
| `MQTT_BROKER_URL` | Broker the bridge connects to (`mqtt://` or `tls://`) | `mqtt://localhost:1883` |
| `MQTT_PUBLIC_HOST` | Broker host returned to devices (`tcp://`/`ws://` only) | `tcp://localhost:8883` |
| `MQTT_USERNAME` / `MQTT_PASSWORD` | Bridge's own broker credentials | - |
| `MQTT_CA_CERT_FILE` | PEM CA bundle to trust for broker TLS | - |
| `JWT_PRIVATE_KEY_FILE` | RSA private key for minting device credentials | - |
| `JWT_TTL` | Lifetime of minted device tokens | `1h` |
| `DJI_APP_ID` / `DJI_APP_KEY` / `DJI_LICENSE` | DJI developer credentials for JSBridge | - |
| `PLATFORM_NAME` | Cloud portal name shown in Pilot 2 | `RescueStream` |
| `WORKSPACE_ID` | Workspace UUID (tenant identifier) | - |
| `METRICS_ENABLED` | Enable Prometheus metrics | `true` |
| `METRICS_PORT` | Port for the metrics endpoint | `8081` |
| `LOCAL` | Use the OTLP gRPC exporter instead of Prometheus | `false` |
| `TRACING_ENABLED` | Enable distributed tracing | `false` |
| `TRACING_SAMPLERATE` | Trace sampling rate | `0.01` |
| `TRACING_SERVICE` | Service name for traces | `rescuestream-dji-bridge` |
| `TRACING_VERSION` | Service version for traces | - |

## Getting Started

### Run the local stack

```bash
make certs          # generate the JWT keypair + a self-signed TLS cert
docker compose up    # EMQX broker + bridge + Grafana LGTM
```

- **Onboarding/telemetry API**: http://localhost:8080
- **EMQX dashboard**: http://localhost:18083
- **Grafana**: http://localhost:3000
- **Metrics**: http://localhost:8081

### Exercise it without hardware

`mock-rcplus` simulates an RC Plus + M30T — onboarding, topology, and live OSD:

```bash
docker compose --profile mock up        # includes the simulator
# or run it against a running stack:
go run ./cmd/mock-rcplus
```

Then watch telemetry flow:

```bash
curl -H 'X-API-Key: dev-integration-key' http://localhost:8080/api/devices
```

### End-to-end test

```bash
make integration     # brings up the stack + mock, asserts telemetry is ingested
```

## Project structure

```
.
├── cmd/
│   ├── rescuestream-dji-bridge/  # bridge entrypoint
│   └── mock-rcplus/              # RC Plus + M30T simulator
├── internal/
│   ├── config/  logging/         # configuration and logging
│   ├── mqtt/                     # autopaho MQTT 5.0 client wrapper
│   ├── cloudapi/                 # DJI Cloud API protocol layer
│   │   ├── message/ topic/       # envelope, topic parsing + router
│   │   ├── correlation/          # tid request/reply correlation
│   │   ├── telemetry/            # OSD/state typed structs
│   │   ├── live/                 # P2 live streaming (stub)
│   │   └── drc/                  # P3 DRC control (stub)
│   ├── device/                   # device registry + telemetry store
│   ├── handler/                  # inbound topic handlers
│   └── onboarding/               # HTTP server + JWT credential minting
├── web/onboarding/               # JSBridge onboarding page
├── docker/                       # EMQX certs, JWT keys, Grafana provisioning
└── scripts/                      # integration test
```

## CI/CD

Pull requests are validated with:

- **commitlint**: Conventional commit message enforcement
- **golangci-lint**: Go linting
- **yamllint**: YAML linting
- **hadolint**: Dockerfile linting
- **go test**: Unit tests
