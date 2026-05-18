package handler

import (
	"context"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

// DRC handles thing/product/{sn}/drc/up uplink messages — heartbeat replies,
// obstacle/HSI info, and image-transmission delay (§8.3).
//
// P3 scaffold: it logs uplink traffic so the subscription is not dead; full
// DRC session handling is future work.
type DRC struct {
	logger *slog.Logger
}

// NewDRC creates a DRC uplink handler.
func NewDRC(logger *slog.Logger) *DRC {
	return &DRC{logger: logger}
}

// Handle implements topic.Handler.
func (h *DRC) Handle(_ context.Context, t topic.Topic, env *message.Envelope) error {
	h.logger.Debug("drc uplink",
		slog.String("sn", t.SN),
		slog.String("method", env.Method))
	return nil
}
