package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/telemetry"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/device"
)

// OSD handles thing/product/{sn}/osd telemetry: it decodes the payload and
// records the latest snapshot in the device store (§6.1).
type OSD struct {
	registry *device.Registry
	store    *device.Store
	logger   *slog.Logger
}

// NewOSD creates an OSD telemetry handler.
func NewOSD(registry *device.Registry, store *device.Store, logger *slog.Logger) *OSD {
	return &OSD{registry: registry, store: store, logger: logger}
}

// Handle implements topic.Handler.
func (h *OSD) Handle(_ context.Context, t topic.Topic, env *message.Envelope) error {
	if h.registry.Kind(t.SN) == device.KindGateway {
		var osd telemetry.GatewayOSD
		if err := env.DecodeData(&osd); err != nil {
			return fmt.Errorf("gateway osd: %w", err)
		}
		h.store.SetOSD(t.SN, device.KindGateway, osd)
		return nil
	}

	// Aircraft, or a device not yet seen in topology — treat as the M30T.
	var osd telemetry.AircraftOSD
	if err := env.DecodeData(&osd); err != nil {
		return fmt.Errorf("aircraft osd: %w", err)
	}
	h.store.SetOSD(t.SN, device.KindAircraft, osd)
	h.logger.Debug("aircraft osd",
		slog.String("sn", t.SN),
		slog.String("mode", osd.Mode()),
		slog.Int("battery", osd.Battery.CapacityPercent))
	return nil
}
