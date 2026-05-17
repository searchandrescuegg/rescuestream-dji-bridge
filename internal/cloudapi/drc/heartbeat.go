package drc

import (
	"context"
	"log/slog"
	"time"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

// DefaultHeartbeatInterval is how often heart_beat is published. A DRC session
// drops if the gap exceeds ~60s (§8.2).
const DefaultHeartbeatInterval = 5 * time.Second

// Publisher publishes a payload to an MQTT topic.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Heartbeat keeps a DRC session alive by publishing periodic heart_beat
// messages on the gateway's DRC downlink (§8.2).
type Heartbeat struct {
	pub       Publisher
	gatewaySN string
	interval  time.Duration
	logger    *slog.Logger
}

// NewHeartbeat creates a Heartbeat for a gateway's DRC session.
func NewHeartbeat(pub Publisher, gatewaySN string, logger *slog.Logger) *Heartbeat {
	return &Heartbeat{
		pub:       pub,
		gatewaySN: gatewaySN,
		interval:  DefaultHeartbeatInterval,
		logger:    logger,
	}
}

// Run publishes heartbeats until ctx is cancelled. It is intended to run in
// its own goroutine for the lifetime of an active DRC session.
func (h *Heartbeat) Run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.beat(ctx); err != nil {
				h.logger.Warn("drc heartbeat failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (h *Heartbeat) beat(ctx context.Context) error {
	env, err := message.New(MethodHeartBeat, map[string]any{
		"seq":       time.Now().UnixMilli(),
		"timestamp": time.Now().UnixMilli(),
	})
	if err != nil {
		return err
	}
	env.Gateway = h.gatewaySN
	payload, err := env.Encode()
	if err != nil {
		return err
	}
	return h.pub.Publish(ctx, topic.DRCDown(h.gatewaySN).String(), payload)
}
