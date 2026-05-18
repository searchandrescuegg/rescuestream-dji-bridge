-- Telemetry archive schema. Applied idempotently at startup.
-- Typed columns for the common fields; the full message data is kept in `raw`
-- (JSONB) so nothing is lost when DJI firmware adds or changes fields.

CREATE TABLE IF NOT EXISTS aircraft_osd (
    id               BIGSERIAL PRIMARY KEY,
    device_sn        TEXT NOT NULL,
    device_ts        BIGINT,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    mode_code        INT,
    latitude         DOUBLE PRECISION,
    longitude        DOUBLE PRECISION,
    height           DOUBLE PRECISION,
    elevation        DOUBLE PRECISION,
    attitude_head    DOUBLE PRECISION,
    attitude_pitch   DOUBLE PRECISION,
    attitude_roll    DOUBLE PRECISION,
    horizontal_speed DOUBLE PRECISION,
    vertical_speed   DOUBLE PRECISION,
    home_distance    DOUBLE PRECISION,
    home_latitude    DOUBLE PRECISION,
    home_longitude   DOUBLE PRECISION,
    wind_speed       DOUBLE PRECISION,
    wind_direction   INT,
    battery_percent  INT,
    control_source   TEXT,
    raw              JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS aircraft_osd_sn_time ON aircraft_osd (device_sn, received_at DESC);

CREATE TABLE IF NOT EXISTS gateway_osd (
    id               BIGSERIAL PRIMARY KEY,
    device_sn        TEXT NOT NULL,
    device_ts        BIGINT,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    capacity_percent INT,
    latitude         DOUBLE PRECISION,
    longitude        DOUBLE PRECISION,
    height           DOUBLE PRECISION,
    raw              JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS gateway_osd_sn_time ON gateway_osd (device_sn, received_at DESC);

CREATE TABLE IF NOT EXISTS device_state (
    id          BIGSERIAL PRIMARY KEY,
    device_sn   TEXT NOT NULL,
    kind        TEXT,
    device_ts   BIGINT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw         JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS device_state_sn_time ON device_state (device_sn, received_at DESC);

CREATE TABLE IF NOT EXISTS device_events (
    id          BIGSERIAL PRIMARY KEY,
    device_sn   TEXT NOT NULL,
    method      TEXT,
    need_reply  INT,
    device_ts   BIGINT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw         JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS device_events_sn_time ON device_events (device_sn, received_at DESC);

CREATE TABLE IF NOT EXISTS topology (
    id          BIGSERIAL PRIMARY KEY,
    gateway_sn  TEXT NOT NULL,
    aircraft_sn TEXT,
    online      BOOLEAN,
    device_ts   BIGINT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw         JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS topology_gw_time ON topology (gateway_sn, received_at DESC);
