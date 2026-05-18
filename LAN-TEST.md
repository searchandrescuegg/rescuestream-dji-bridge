# LAN hardware test runbook

How to test the bridge against a real DJI RC Plus + M30T on a local network.
Covers MVP phases **P0** (onboarding) and **P1** (live telemetry).

## Prerequisites

- **DJI developer credentials** — `app_id`, `app_key`, `license` from the DJI
  developer site. The onboarding page's first call is `platformVerifyLicense`;
  the whole flow stops if it fails. **Confirm you have these before starting.**
- A host (laptop) with Docker, on the **same LAN / Wi-Fi** as the RC Plus.
- The RC Plus running DJI Pilot 2, paired with a powered-on M30T.

## 1. Configure

```bash
cp .env.example .env
```

Edit `.env`:

- `MQTT_PUBLIC_HOST` — `tcp://<host-lan-ip>:1883`, the bridge host's LAN IP.
  Find it: `ipconfig getifaddr en0` (macOS) or `hostname -I` (Linux).
  Must be `tcp://` (not `tls://`) and the real IP (not `localhost`).
- `DJI_APP_ID`, `DJI_APP_KEY`, `DJI_LICENSE`, `WORKSPACE_ID` — your DJI values.
- `SN_ALLOWLIST` — your RC Plus serial number (or leave blank to allow any).
- `API_KEY` — change it from the dev default to any string you choose.

## 2. Start the stack

```bash
make certs          # once — generates the JWT signing keypair
docker compose up   # EMQX broker + bridge + Grafana
```

The bridge listens on `:8080` (HTTP) and EMQX on `:1883` — both published on
the host's LAN IP. The RC Plus connects in plaintext, so no TLS certificate is
needed for a LAN test.

> **macOS:** if the controller can't reach the host, check System Settings →
> Network → Firewall is not blocking incoming connections.

## 3. Onboard from Pilot 2

On the RC Plus, in DJI Pilot 2: **Cloud Services → add a platform → enter URL**:

```
http://<host-lan-ip>:8080/?key=<API_KEY>
```

Pilot loads the page in its WebView. The page runs the JSBridge flow and prints
each step on screen: license verified → platform info set → serial numbers read
→ broker credentials received → cloud module loaded → connected.

## 4. Verify

- **EMQX dashboard** — `http://<host-lan-ip>:18083` (login `admin` / `public`):
  the RC Plus appears as a connected client.
- **Bridge logs** — `docker compose logs -f main`: look for `topology updated`,
  then `aircraft osd`.
- **Telemetry API**:
  ```bash
  curl -H "X-API-Key: <API_KEY>" http://localhost:8080/api/devices
  ```
  The M30T should appear with live position, battery, and `mode_code`.

**P0** is proven once topology is mapped; **P1** once live OSD updates appear.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Page stops at "license verify failed" | Wrong / missing `DJI_*` credentials |
| Page can't reach the backend | Wrong host IP, not on the same LAN, or host firewall |
| "loadComponent failed" / `connectCallback` never fires | `MQTT_PUBLIC_HOST` wrong, or the RC Plus cannot reach `:1883` |
| Connected, but `/api/devices` shows nothing | Check `docker compose logs main` for `update_topo`; the JSBridge page may need adjustment for real firmware |
| `/api/devices` returns 401 | `X-API-Key` missing or not matching `.env` `API_KEY` |

The JSBridge onboarding page (`web/onboarding/index.html`) has not been run
against real hardware before — if it stalls, the on-screen step log shows
exactly where, and that file is where to adjust the JSBridge calls.
