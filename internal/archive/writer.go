// Package archive persists downlinked DJI telemetry to Postgres for later
// playback. Writes are batched on a background goroutine so telemetry
// ingestion never blocks on the database.
package archive

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/telemetry"
)

//go:embed schema.sql
var schemaSQL string

// Writer defaults.
const (
	defaultBatchSize     = 200
	defaultFlushInterval = 2 * time.Second
	defaultQueueSize     = 2000
	flushTimeout         = 10 * time.Second
)

// record is one queued telemetry row. Concrete types below.
type record interface{ isRecord() }

type aircraftOSDRecord struct {
	deviceSN string
	deviceTS int64
	osd      telemetry.AircraftOSD
	raw      []byte
}

type gatewayOSDRecord struct {
	deviceSN string
	deviceTS int64
	osd      telemetry.GatewayOSD
	raw      []byte
}

type stateRecord struct {
	deviceSN string
	kind     string
	deviceTS int64
	raw      []byte
}

type eventRecord struct {
	deviceSN  string
	method    string
	needReply int
	deviceTS  int64
	raw       []byte
}

type topologyRecord struct {
	gatewaySN  string
	aircraftSN string
	online     bool
	deviceTS   int64
	raw        []byte
}

func (aircraftOSDRecord) isRecord() {}
func (gatewayOSDRecord) isRecord()  {}
func (stateRecord) isRecord()       {}
func (eventRecord) isRecord()       {}
func (topologyRecord) isRecord()    {}

// sink flushes a batch of records to durable storage.
type sink interface {
	write(ctx context.Context, batch []record)
}

// Writer batches downlinked telemetry into Postgres on a background goroutine.
type Writer struct {
	databaseURL   string
	batchSize     int
	flushInterval time.Duration
	queueSize     int
	logger        *slog.Logger

	pool    *pgxpool.Pool
	sink    sink
	queue   chan record
	stop    chan struct{}
	done    chan struct{}
	dropped atomic.Int64
}

// Option configures a Writer; see the With* constructors.
type Option func(*Writer)

// WithLogger sets the structured logger (defaults to slog.Default()).
func WithLogger(l *slog.Logger) Option { return func(w *Writer) { w.logger = l } }

// WithBatchSize sets how many rows are buffered before a flush.
func WithBatchSize(n int) Option { return func(w *Writer) { w.batchSize = n } }

// WithFlushInterval sets the maximum time rows wait before being flushed.
func WithFlushInterval(d time.Duration) Option { return func(w *Writer) { w.flushInterval = d } }

// New creates a telemetry archive Writer for the given Postgres URL. Call
// Connect to open the pool and start the background writer.
func New(databaseURL string, opts ...Option) *Writer {
	w := &Writer{
		databaseURL:   databaseURL,
		batchSize:     defaultBatchSize,
		flushInterval: defaultFlushInterval,
		queueSize:     defaultQueueSize,
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	w.queue = make(chan record, w.queueSize)
	w.stop = make(chan struct{})
	w.done = make(chan struct{})
	return w
}

// Connect opens the connection pool (with OpenTelemetry query tracing), applies
// the schema, and starts the background batch writer.
func (w *Writer) Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(w.databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return fmt.Errorf("apply schema: %w", err)
	}
	w.pool = pool
	w.sink = &pgxSink{pool: pool, logger: w.logger}
	go w.run()
	return nil
}

// Close flushes buffered telemetry and closes the pool.
func (w *Writer) Close(ctx context.Context) {
	if w.pool == nil {
		return
	}
	close(w.stop)
	select {
	case <-w.done:
	case <-ctx.Done():
		w.logger.Warn("archive close timed out before final flush")
	}
	w.pool.Close()
}

// AircraftOSD queues an M30T OSD report for archival.
func (w *Writer) AircraftOSD(deviceSN string, deviceTS int64, osd telemetry.AircraftOSD, raw []byte) {
	w.enqueue(aircraftOSDRecord{deviceSN: deviceSN, deviceTS: deviceTS, osd: osd, raw: raw})
}

// GatewayOSD queues an RC Plus OSD report for archival.
func (w *Writer) GatewayOSD(deviceSN string, deviceTS int64, osd telemetry.GatewayOSD, raw []byte) {
	w.enqueue(gatewayOSDRecord{deviceSN: deviceSN, deviceTS: deviceTS, osd: osd, raw: raw})
}

// State queues a device state message for archival.
func (w *Writer) State(deviceSN, kind string, deviceTS int64, raw []byte) {
	w.enqueue(stateRecord{deviceSN: deviceSN, kind: kind, deviceTS: deviceTS, raw: raw})
}

// Event queues a device event for archival.
func (w *Writer) Event(deviceSN, method string, needReply int, deviceTS int64, raw []byte) {
	w.enqueue(eventRecord{deviceSN: deviceSN, method: method, needReply: needReply, deviceTS: deviceTS, raw: raw})
}

// Topology queues a topology update for archival.
func (w *Writer) Topology(gatewaySN, aircraftSN string, online bool, deviceTS int64, raw []byte) {
	w.enqueue(topologyRecord{gatewaySN: gatewaySN, aircraftSN: aircraftSN, online: online, deviceTS: deviceTS, raw: raw})
}

// enqueue performs a non-blocking send so telemetry ingestion never blocks on
// the database; a full queue increments the dropped counter.
func (w *Writer) enqueue(r record) {
	select {
	case w.queue <- r:
	default:
		w.dropped.Add(1)
	}
}

// run is the background batch writer: it accumulates records and flushes them
// on batch size, on the flush interval, and on shutdown.
func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]record, 0, w.batchSize)
	flush := func() {
		if len(batch) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
			w.sink.write(ctx, batch)
			cancel()
			batch = batch[:0]
		}
		if d := w.dropped.Swap(0); d > 0 {
			w.logger.Warn("archive queue full — telemetry dropped", slog.Int64("count", d))
		}
	}
	for {
		select {
		case r := <-w.queue:
			batch = append(batch, r)
			if len(batch) >= w.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.stop:
			for {
				select {
				case r := <-w.queue:
					batch = append(batch, r)
				default:
					flush()
					return
				}
			}
		}
	}
}
