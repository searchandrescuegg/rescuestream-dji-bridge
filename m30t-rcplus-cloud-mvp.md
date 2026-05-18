# M30T + RC Plus — Cloud Integration MVP

**Scope:** DJI Matrice 30T flown on a DJI RC Plus, integrated to a self-hosted cloud
platform over the **pilot-to-cloud** Cloud API (MQTT). This document covers *only* the
RC Plus / pilot path. Dock modes (autonomous flight, wayline missions, dock telemetry)
are explicitly **out of scope** — see [§9](#9-explicitly-out-of-scope).

**Target backend:** Go service (MQTT 5.0 client + HTTP server for onboarding).

---

## 1. Goal

Stand up the smallest system that proves the full loop:

1. An operator onboards the RC Plus to our cloud from DJI Pilot 2.
2. Our Go backend receives live M30T telemetry over MQTT.
3. Our backend can start/stop a live video stream.
4. Our backend can request cloud control and drive the **payload** (camera/gimbal) —
   with operator consent.

If all four work, the MVP is done. Programmatic *flight* is not part of this MVP and is
not supported on the RC Plus at all (see §9).

---

## 2. Prerequisites

| Item | Value / Note |
|---|---|
| Aircraft | Matrice 30 / 30T — device `type=67`, `sub_type=0` (M30) or `1` (M30T) |
| Gateway | DJI RC Plus + DJI Pilot 2 — device `type=119`, `sub_type=0` |
| M30T camera payload index | `53-0-0` (M30 is `52-0-0`; FPV camera is `39-0-7`) |
| Developer credentials | `app_id`, `app_key`, `license` — request on the DJI developer site; required for JSBridge auth |
| MQTT broker | EMQX (or compatible), MQTT **5.0**, TLS on `:8883` |
| Object storage | S3-compatible bucket for media upload (optional for MVP, see §8) |
| TLS | Broker must present a cert the RC Plus trusts — a public-CA cert keyed to the broker hostname (e.g. Let's Encrypt); see [§5.3](#53-tls-certificate) |

**Key mental model:** the **RC Plus is the gateway**. In every topic below,
`{gateway_sn}` = RC Plus SN. The **M30T is the sub-device**; `{device_sn}` = aircraft SN.
Telemetry is published *per device SN*; commands are published *to the gateway SN*.

---

## 3. Architecture

```
 DJI Pilot 2 (on RC Plus)
   │  1. operator opens our web URL in Pilot's WebView
   │  2. JSBridge: verify license, load "thing" (cloud) module
   ▼
 ┌─────────────────────┐         ┌──────────────────────────┐
 │  Onboarding Web App │  HTTP   │   Go Backend             │
 │  (served by us)     │◄───────►│  - HTTP: serve web app   │
 └─────────────────────┘         │  - issue MQTT creds      │
   │ JSBridge returns broker      │  - MQTT 5.0 client       │
   │ host + username + password   │  - topic router         │
   ▼                              │  - tid/bid correlation   │
 RC Plus MQTT client ──TLS──► EMQX Broker ◄──────────────────┘
                                  ▲
                                  │ live video (RTMP/GB28181/Agora)
                            Media server / streaming endpoint
```

The RC Plus and our Go backend are both MQTT clients on the same broker. They never talk
directly — the broker is the message bus.

---

## 4. MVP milestones

Build in this order. Each phase is independently demoable.

| Phase | Deliverable | Proves |
|---|---|---|
| **P0** | Onboarding + MQTT connect | RC Plus appears online; topology received |
| **P1** | Telemetry ingestion (read-only) | We see live M30T position/battery/state |
| **P2** | Live streaming start/stop | We can pull M30T video on demand |
| **P3** | DRC cloud control — payload only | We can take photos / move gimbal with consent |

P0+P1 deliver real value on their own (a live fleet dashboard). P2/P3 are additive.

---

## 5. Phase 0 — Onboarding & connection

### 5.1 JSBridge onboarding flow

The operator opens **Pilot 2 → Cloud Services → enter our platform URL**. Pilot loads
our web page in an embedded WebView. Our page's JS then:

1. `window.djiBridge.platformVerifyLicense(appId, appKey, license)` — **must succeed
   before any other JSBridge call**. Check with `platformIsVerified()`.
2. `platformSetInformation(platformName, workspaceName, desc)` — sets the cloud portal
   labels shown in Pilot.
3. `platformSetWorkspaceId(uuid)` — workspace UUID (our tenant identifier).
4. Read SNs: `platformGetRemoteControllerSN()` and `platformGetAircraftSN()`.
5. Call our Go backend (HTTP) with those SNs → backend returns MQTT broker host +
   per-device username/password.
6. `platformLoadComponent("thing", {host, username, password, connectCallback})` —
   loads the **Cloud (thing) module**, which makes the RC Plus connect to our broker.
   `host` must be `tcp://host:port` or `ws://host:port` (scheme is required).

All JSBridge return values are JSON **strings** — `JSON.parse(res)` them; `code:0` = ok.

> The full interface list is in `docs/en/60.api-reference/10.pilot-to-cloud/30.jsbridge.md`.

### 5.2 MQTT connection

- Protocol: MQTT **5.0** over TLS (`:8883`).
- Each device authenticates with the username/password our backend issued in step 5.
- Our Go backend connects as a separate client with its own credentials.
- Recommended client library (Go): `eclipse/paho.golang` (paho MQTT 5.0 client).

### 5.3 TLS certificate

The `:8883` listener needs a cert the RC Plus trusts. The RC Plus trust store is
fixed (closed device) — it trusts the common public roots, so a **public-CA cert
keyed to the broker hostname works out of the box**. There is no "MQTT cert":
Let's Encrypt issues an X.509 cert for a *hostname*, and the broker's TLS
listener consumes the same PEM files an HTTPS server would.

**Domain validation:** HTTP-01 validation needs port 80, but the broker is on
8883. Use **DNS-01** instead (a TXT record via a certbot DNS plugin) — no ports
involved, works even if the broker box isn't reachable on 80.

Two ways to wire it up:

- **certbot → EMQX directly.** `certbot certonly --dns-<provider> -d mqtt.example.com`
  produces `privkey.pem` + `fullchain.pem`. Point the listener at them:

  ```hocon
  listeners.ssl.default {
    bind = "0.0.0.0:8883"
    ssl_options {
      certfile = "/etc/emqx/certs/fullchain.pem"
      keyfile  = "/etc/emqx/certs/privkey.pem"
      verify   = verify_none   # RC Plus authenticates by username/password, not a client cert
    }
  }
  ```

  Two gotchas: (1) EMQX runs non-root and can't read `/etc/letsencrypt/live`
  directly — copy and `chown emqx` the files; (2) certs expire every 90 days —
  use a certbot `--deploy-hook` to copy the renewed files and reload EMQX.

- **TLS-terminating proxy.** Put Traefik (TCP router + built-in ACME) in front;
  it obtains, renews, and hot-reloads the cert automatically and forwards to
  EMQX's plaintext `:1883`. Lowest-maintenance option — EMQX never touches a cert.

**Serve the full chain** (`fullchain.pem`, not the bare leaf) so the RC Plus can
chain leaf → Let's Encrypt intermediate → trusted root. The cert hostname must
match the `host` passed to `platformLoadComponent` in §5.1.

### 5.4 Confirm the link — topology

On connect / device power-on the RC Plus publishes to:

```
sys/product/{rcplus_sn}/status        method = update_topo
```

`data.sub_devices[]` lists the paired aircraft (SN, `type`, `sub_type`). An **empty
`sub_devices` array means the aircraft is offline.** This topic is how we learn which
M30T SN belongs to which RC Plus — store this mapping. Reply on
`sys/product/{rcplus_sn}/status_reply`.

**P0 done when:** topology message received, aircraft SN extracted and mapped.

---

## 6. Phase 1 — Telemetry ingestion (read-only)

### 6.1 Topics to subscribe

| Topic | Direction | Contents |
|---|---|---|
| `thing/product/{m30t_sn}/osd` | device → us | High-frequency telemetry (`pushMode=0`), ~periodic |
| `thing/product/{m30t_sn}/state` | device → us | Change-triggered state (`pushMode=1`) |
| `thing/product/{rcplus_sn}/osd` | device → us | RC Plus telemetry (signal, battery) |
| `thing/product/{rcplus_sn}/state` | device → us | RC Plus state changes |
| `thing/product/{rcplus_sn}/events` | device → us | Discrete events + command progress |
| `sys/product/{rcplus_sn}/status` | device → us | Online/offline + topology |

### 6.2 Message envelope

Every message shares this envelope:

```json
{
  "tid": "transaction-uuid",
  "bid": "business-uuid",
  "timestamp": 1667220873846,
  "gateway": "{rcplus_sn}",
  "data": { },
  "method": "..."        // present on services/events/state, not on plain osd
}
```

- `tid` — transaction id; **correlate replies to requests by `tid`**.
- `bid` — business id; groups a multi-message business flow.
- `method` — the operation name (e.g. `update_topo`, `live_start_push`).

### 6.3 Useful M30T OSD fields (`thing/product/{m30t_sn}/osd`)

| Field | Meaning |
|---|---|
| `mode_code` | Aircraft state enum — `3`=Manual flight, `9`=RTH, `10`=Landing, `14`=Not connected, `17`=Live flight controls (DRC) |
| `latitude` / `longitude` / `height` / `elevation` | Position; `height` is ellipsoidal, `elevation` is relative to takeoff |
| `attitude_head` / `attitude_pitch` / `attitude_roll` | Aircraft attitude |
| `horizontal_speed` / `vertical_speed` | Velocity |
| `home_distance` / `home_latitude` / `home_longitude` | Home point |
| `battery` | Battery struct (percent, voltage, temperature) |
| `wind_speed` / `wind_direction` | Estimated wind (reference only) |
| `position_state` | GNSS / RTK fix quality |
| `control_source` | Current control source — `A`/`B` for RC slots, a UUID for a browser/cloud session |
| `{53-0-0}.gimbal_pitch/roll/yaw` | M30T gimbal attitude (keyed by payload index) |

> Full thing-model field list:
> `docs/en/60.api-reference/10.pilot-to-cloud/00.mqtt/30.others/10.aircraft/00.properties.md`

### 6.4 Go ingestion design

- One paho MQTT 5.0 client; subscribe with a single wildcard
  `thing/product/+/osd` etc., then route on the SN segment.
- A `topicRouter` that dispatches on `(suffix, method)` → handler.
- Decode into typed structs; tolerate unknown fields (DJI adds fields across firmware).
- Persist latest-known state per device (in-memory map + optional TimescaleDB/Postgres
  for history).

**P1 done when:** a dashboard shows live M30T position, battery, and `mode_code` updating
in real time.

---

## 7. Phase 2 — Live streaming

All four methods are **services** published by us to
`thing/product/{rcplus_sn}/services`; the RC Plus acks on `.../services_reply` (match by
`tid`).

| Method | Purpose |
|---|---|
| `live_start_push` | Start a stream from a camera |
| `live_stop_push` | Stop a stream |
| `live_set_quality` | Change quality of a running stream |
| `live_lens_change` | Switch lens (`normal` / `zoom` / `wide` / `thermal`) |

### 7.1 `live_start_push` payload

```json
{
  "tid": "uuid",
  "bid": "uuid",
  "timestamp": 1654070968655,
  "data": {
    "url_type": 1,
    "url": "rtmp://media.example.com/live/m30t",
    "video_id": "{m30t_sn}/53-0-0/normal-0",
    "video_quality": 0
  }
}
```

- `url_type`: `0`=Agora, `1`=RTMP, `3`=GB28181.
- `video_id`: `{sn}/{camera_index}/{video_index}` — M30T main camera is `53-0-0`.
  Use `normal-0`, or the thermal stream index for IR.
- `video_quality`: `0`=Adaptive, `1`=Smooth, `2`=SD, `3`=HD, `4`=UHD.
- For Agora, URL-encode the token once (`+` etc. break Pilot's parser otherwise).

**MVP recommendation:** use **RTMP** (`url_type=1`). It is the simplest to self-host —
stand up an RTMP-capable media server (e.g. MediaMTX / nginx-rtmp) and point `url` at it.
GB28181 is heavier; Agora adds a third-party dependency.

> Reference: `docs/en/60.api-reference/10.pilot-to-cloud/00.mqtt/20.rc-pro/20.live.md`

**P2 done when:** a `live_start_push` from our backend produces playable video on our
media server, and `live_stop_push` ends it.

---

## 8. Phase 3 — DRC cloud control (payload only)

DRC ("Direct Remote Control") on the RC Plus lets the cloud drive the **camera and
gimbal**. It does **not** let the cloud fly the aircraft (see §9).

### 8.1 Consent + session setup sequence

```
1. us → services         cloud_control_auth_request   {user_id, user_callsign, control_keys:["flight"]}
       ↳ a "Request Authorization" pop-up appears on the RC Plus screen
2. RC → services_reply   cloud_control_auth_request   (immediate ack)
3. RC → events           cloud_control_auth_notify    {output.status: "ok"|"failed"|"canceled"}
       ↳ "ok" only after the operator taps Accept. GATE everything below on this.
4. us → services         drc_mode_enter               {mqtt_broker:{...}, osd_frequency, hsi_frequency}
5. RC → services_reply   drc_mode_enter               {result:0}
6. RC → events           drc_status_notify            {drc_state: 0|1|2}   (2 = Connected)
   ... DRC session is now live ...
7. us → services         cloud_control_release        (when finished)
```

- `cloud_control_auth_notify.status`:
  `ok` = operator agreed, `failed` = error/denied, `canceled` = superseded by another
  request. Treat anything but `ok` as a hard stop.
- `drc_mode_enter.mqtt_broker` provides a (possibly separate) broker connection for the
  high-frequency DRC link, with a JWT-style `password` and `expire_time`. Credentials are
  reusable until expiry.

### 8.2 Heartbeat — mandatory

Once in DRC mode, exchange `heart_beat` on the DRC link:

```
us → thing/product/{rcplus_sn}/drc/down   method=heart_beat
RC → thing/product/{rcplus_sn}/drc/up     method=heart_beat
```

**If the gap exceeds ~60 s the DRC session drops.** Send on a 1–10 s ticker.

### 8.3 DRC upward push topics (`thing/product/{rcplus_sn}/drc/up`)

| Method | Contents |
|---|---|
| `heart_beat` | Heartbeat reply |
| `hsi_info_push` | Obstacle / horizontal-situation info |
| `delay_info_push` | Image-transmission link delay |

### 8.4 Payload commands (services topic, during DRC)

Published to `thing/product/{rcplus_sn}/services`, acked on `.../services_reply`:

| Category | Methods |
|---|---|
| Capture | `camera_mode_switch`, `camera_photo_take`, `camera_photo_stop`, `camera_recording_start`, `camera_recording_stop` |
| Framing | `camera_aim`, `camera_look_at`, `camera_screen_drag`, `camera_focal_length_set`, `camera_frame_zoom`, `camera_screen_split`, `gimbal_reset` |
| Exposure/focus | `camera_exposure_set`, `camera_exposure_mode_set`, `camera_focus_value_set`, `camera_focus_mode_set`, `camera_point_focus_action` |
| Thermal (M30T) | `ir_metering_mode_set`, `ir_metering_point_set`, `ir_metering_area_set` |
| Storage | `photo_storage_set`, `video_storage_set` |

Progress for long ops arrives on `events`, e.g. `camera_photo_take_progress`
(`status: in_progress|ok|fail`).

> Reference: `docs/en/60.api-reference/10.pilot-to-cloud/00.mqtt/20.rc-pro/30.drc.md`

**P3 done when:** with operator consent, our backend can switch camera mode, take a
photo, and reset/aim the gimbal — and correctly aborts when consent is denied.

---

## 9. Explicitly out of scope

These exist for the **Dock**, not the RC Plus. Do **not** design around them:

- **Virtual joystick / `drone_control`** — no cloud-side flight control on RC Plus. A
  human pilot flies; the cloud only controls the payload.
- **`takeoff_to_point`, `fly_to_point`, `fly_to_point_update`, `fly_to_point_stop`** —
  dock-only autonomous flight.
- **Wayline / waypoint missions** — dock-only.
- **Dock OSD** (rain, dock cover, charge state, etc.) — no dock hardware.

If true autonomous flight is ever required, that is a **Dock** project, not RC Plus.

---

## 10. Go backend — implementation notes

- **MQTT client:** `eclipse/paho.golang` (MQTT 5.0). One long-lived client for the main
  bus; a second for the DRC link if `drc_mode_enter` returns a different broker.
- **Topic router:** parse `{prefix}/product/{sn}/{suffix}`; dispatch on `(suffix, method)`.
- **Request/reply correlation:** generate a UUID `tid` per command; keep a
  `map[tid]chan reply` with a timeout (commands that get no `services_reply` within
  ~10 s should fail).
- **State machine per device session:**
  `OFFLINE → ONLINE → (DRC: AUTH_PENDING → AUTH_OK → DRC_ACTIVE) → ...`
  Gate every payload/DRC command on `DRC_ACTIVE`. Drop to `ONLINE` on
  `drc_status_notify.drc_state != 2` or heartbeat loss.
- **Heartbeat goroutine:** per active DRC session, ticker + context cancellation.
- **Credential issuance:** the HTTP onboarding endpoint mints per-device MQTT
  username/password (and the DRC JWT). Keep them short-lived.
- **Idempotency / firmware drift:** decode JSON leniently; never hard-fail on unknown
  fields — DJI adds them across firmware versions.

---

## 11. MVP test checklist

- [ ] License verifies in Pilot 2 WebView (`platformIsVerified()` → true).
- [ ] `platformLoadComponent("thing", …)` connects; `connectCallback` reports success.
- [ ] `update_topo` received; M30T SN extracted; offline → empty `sub_devices` handled.
- [ ] `osd` telemetry flowing from the M30T SN; dashboard updates live.
- [ ] `mode_code` transitions observed (standby → manual flight → landing).
- [ ] `live_start_push` (RTMP) yields playable video; `live_stop_push` ends it.
- [ ] `live_lens_change` to `thermal` switches the M30T to the IR camera.
- [ ] `cloud_control_auth_request` raises the pop-up; **Accept** → `auth_notify status:ok`.
- [ ] **Deny** → `status:failed`; backend aborts cleanly, no commands sent.
- [ ] `drc_mode_enter` → `drc_status_notify drc_state:2`.
- [ ] Heartbeat sustained; stopping it for >60 s drops DRC as expected.
- [ ] `camera_photo_take` succeeds; `camera_photo_take_progress` reaches `ok`.
- [ ] `cloud_control_release` cleanly ends the session.

---

## 12. Open questions / risks

1. **TLS trust** — the RC Plus trust store is closed; a public-CA cert (Let's Encrypt,
   see §5.3) should be accepted out of the box. Confirm against the actual controller
   firmware before relying on it.
2. **Media server choice** — RTMP (MediaMTX) is simplest; confirm latency is acceptable
   for the use case before committing.
3. **Consent UX** — every DRC session needs a physical tap on the RC Plus. Any "remote,
   unattended" expectation is incompatible with the RC Plus and implies a Dock.
4. **DRC broker** — confirm whether `drc_mode_enter` returns the same broker or a
   dedicated relay; the Go client design must handle both.
5. **Firmware versions** — pin tested RC Plus / M30T firmware; field-test before relying
   on any specific OSD field.
6. **Multi-tenant** — workspace UUID + per-device credentials must isolate tenants on a
   shared broker.

---

## 13. Reference docs

All under `docs/en/60.api-reference/10.pilot-to-cloud/` in the `Cloud-API-Doc` repo:

| Topic | Path |
|---|---|
| Topic definitions & envelope | `00.mqtt/00.topic-definition.md` |
| Aircraft (M30T) properties | `00.mqtt/30.others/10.aircraft/00.properties.md` |
| RC Plus properties | `00.mqtt/20.rc-pro/00.properties.md` |
| RC Plus device/topology | `00.mqtt/20.rc-pro/10.device.md` |
| Live streaming | `00.mqtt/20.rc-pro/20.live.md` |
| DRC (auth, payload control) | `00.mqtt/20.rc-pro/30.drc.md` |
| Remote control extras | `00.mqtt/20.rc-pro/40.remote-control.md` |
| JSBridge (onboarding) | `30.jsbridge.md` |
| Product support matrix | `docs/en/10.overview/30.product-support.md` |
