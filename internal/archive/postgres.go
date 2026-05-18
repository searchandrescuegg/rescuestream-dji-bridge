package archive

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgxSink writes batches of telemetry records to Postgres via pgx.
type pgxSink struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

const (
	insertAircraftOSD = `INSERT INTO aircraft_osd
(device_sn, device_ts, mode_code, latitude, longitude, height, elevation,
 attitude_head, attitude_pitch, attitude_roll, horizontal_speed, vertical_speed,
 home_distance, home_latitude, home_longitude, wind_speed, wind_direction,
 battery_percent, control_source, raw)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`

	insertGatewayOSD = `INSERT INTO gateway_osd
(device_sn, device_ts, capacity_percent, latitude, longitude, height, raw)
VALUES ($1,$2,$3,$4,$5,$6,$7)`

	insertState = `INSERT INTO device_state (device_sn, kind, device_ts, raw)
VALUES ($1,$2,$3,$4)`

	insertEvent = `INSERT INTO device_events (device_sn, method, need_reply, device_ts, raw)
VALUES ($1,$2,$3,$4,$5)`

	insertTopology = `INSERT INTO topology (gateway_sn, aircraft_sn, online, device_ts, raw)
VALUES ($1,$2,$3,$4,$5)`
)

// write inserts a batch of records — one queued statement per row, sent in a
// single round-trip. Archival is best-effort: errors are logged, not propagated.
func (s *pgxSink) write(ctx context.Context, batch []record) {
	b := &pgx.Batch{}
	for _, r := range batch {
		switch rec := r.(type) {
		case aircraftOSDRecord:
			o := rec.osd
			b.Queue(insertAircraftOSD,
				rec.deviceSN, rec.deviceTS, o.ModeCode, o.Latitude, o.Longitude,
				o.Height, o.Elevation, o.AttitudeHead, o.AttitudePitch, o.AttitudeRoll,
				o.HorizontalSpeed, o.VerticalSpeed, o.HomeDistance, o.HomeLatitude,
				o.HomeLongitude, o.WindSpeed, o.WindDirection, o.Battery.CapacityPercent,
				o.ControlSource, jsonb(rec.raw))
		case gatewayOSDRecord:
			o := rec.osd
			b.Queue(insertGatewayOSD,
				rec.deviceSN, rec.deviceTS, o.CapacityPercent, o.Latitude,
				o.Longitude, o.Height, jsonb(rec.raw))
		case stateRecord:
			b.Queue(insertState, rec.deviceSN, rec.kind, rec.deviceTS, jsonb(rec.raw))
		case eventRecord:
			b.Queue(insertEvent, rec.deviceSN, rec.method, rec.needReply, rec.deviceTS, jsonb(rec.raw))
		case topologyRecord:
			b.Queue(insertTopology, rec.gatewaySN, rec.aircraftSN, rec.online, rec.deviceTS, jsonb(rec.raw))
		}
	}
	if err := s.pool.SendBatch(ctx, b).Close(); err != nil {
		s.logger.Error("archive batch insert failed",
			slog.Int("rows", len(batch)), slog.String("error", err.Error()))
	}
}

// jsonb returns a value safe to bind to a JSONB column; an empty payload
// becomes the JSON null literal, since an empty string is not valid JSON.
func jsonb(raw []byte) string {
	if len(raw) == 0 {
		return "null"
	}
	return string(raw)
}
