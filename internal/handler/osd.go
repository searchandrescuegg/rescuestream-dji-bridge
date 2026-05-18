package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/archive"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/telemetry"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/device"
)

// OSD handles thing/product/{sn}/osd telemetry: it decodes the payload, records
// the latest snapshot in the device store, and archives it for playback (§6.1).
type OSD struct {
	registry *device.Registry
	store    *device.Store
	archive  *archive.Writer
	logger   *slog.Logger
}

// NewOSD creates an OSD telemetry handler. arc may be nil (persistence off).
func NewOSD(registry *device.Registry, store *device.Store, arc *archive.Writer, logger *slog.Logger) *OSD {
	return &OSD{registry: registry, store: store, archive: arc, logger: logger}
}

// Handle implements topic.Handler.
func (h *OSD) Handle(_ context.Context, t topic.Topic, env *message.Envelope) error {
	if h.registry.Kind(t.SN) == device.KindGateway {
		var osd telemetry.GatewayOSD
		if err := env.DecodeData(&osd); err != nil {
			return fmt.Errorf("gateway osd: %w", err)
		}
		h.store.SetOSD(t.SN, device.KindGateway, osd)
		if h.archive != nil {
			h.archive.GatewayOSD(t.SN, env.Timestamp, osd, env.Data)
		}
		return nil
	}

	// Aircraft, or a device not yet seen in topology — treat as the M30T.
	var osd telemetry.AircraftOSD
	if err := env.DecodeData(&osd); err != nil {
		return fmt.Errorf("aircraft osd: %w", err)
	}
	h.store.SetOSD(t.SN, device.KindAircraft, osd)
	if h.archive != nil {
		h.archive.AircraftOSD(t.SN, env.Timestamp, osd, env.Data)
	}
	h.logger.Debug("aircraft osd",
		slog.String("sn", t.SN),
		slog.String("mode", osd.Mode()),
		slog.Int("battery", osd.Battery.CapacityPercent))
	return nil
}
