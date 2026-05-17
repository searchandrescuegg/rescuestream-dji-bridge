package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/archive"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/device"
)

// State handles thing/product/{sn}/state change-triggered messages. State
// payloads vary widely, so they are stored as a generic map (§6.1). The RC Plus
// subscribes to state_reply and expects each state report to be acknowledged —
// without the ack it treats the cloud as unresponsive and stalls onboarding.
type State struct {
	registry *device.Registry
	store    *device.Store
	pub      Publisher
	archive  *archive.Writer
	logger   *slog.Logger
}

// NewState creates a device-state handler. arc may be nil (persistence off).
func NewState(registry *device.Registry, store *device.Store, pub Publisher, arc *archive.Writer, logger *slog.Logger) *State {
	return &State{registry: registry, store: store, pub: pub, archive: arc, logger: logger}
}

// Handle implements topic.Handler.
func (h *State) Handle(ctx context.Context, t topic.Topic, env *message.Envelope) error {
	var data map[string]any
	if err := env.DecodeData(&data); err != nil {
		return fmt.Errorf("device state: %w", err)
	}
	kind := h.registry.Kind(t.SN)
	h.store.SetState(t.SN, kind, data)
	h.logger.Debug("device state", slog.String("sn", t.SN))
	if h.archive != nil {
		h.archive.State(t.SN, kind, env.Timestamp, env.Data)
	}

	reply, err := env.Reply(map[string]any{"result": 0})
	if err != nil {
		return fmt.Errorf("state reply: %w", err)
	}
	payload, err := reply.Encode()
	if err != nil {
		return fmt.Errorf("state reply: %w", err)
	}
	return h.pub.Publish(ctx, topic.StateReply(t.SN).String(), payload)
}
