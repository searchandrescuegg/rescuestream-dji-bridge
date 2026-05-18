package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/archive"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

// Events handles thing/product/{sn}/events — discrete events and command
// progress. It archives every event and acknowledges any that set need_reply;
// P3 will additionally consume command progress.
type Events struct {
	pub     Publisher
	archive *archive.Writer
	logger  *slog.Logger
}

// NewEvents creates an events handler. arc may be nil (persistence off).
func NewEvents(pub Publisher, arc *archive.Writer, logger *slog.Logger) *Events {
	return &Events{pub: pub, archive: arc, logger: logger}
}

// Handle implements topic.Handler.
func (h *Events) Handle(ctx context.Context, t topic.Topic, env *message.Envelope) error {
	h.logger.Info("device event",
		slog.String("sn", t.SN),
		slog.String("method", env.Method))
	if h.archive != nil {
		h.archive.Event(t.SN, env.Method, env.NeedReply, env.Timestamp, env.Data)
	}

	if env.NeedReply != 1 {
		return nil
	}
	reply, err := env.Reply(map[string]any{"result": 0})
	if err != nil {
		return fmt.Errorf("events reply: %w", err)
	}
	payload, err := reply.Encode()
	if err != nil {
		return fmt.Errorf("events reply: %w", err)
	}
	return h.pub.Publish(ctx, topic.EventsReply(t.SN).String(), payload)
}
