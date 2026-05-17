package topic

import (
	"context"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
)

// Handler processes an inbound message for a parsed topic.
type Handler interface {
	Handle(ctx context.Context, t Topic, env *message.Envelope) error
}

// HandlerFunc adapts an ordinary function to the Handler interface.
type HandlerFunc func(ctx context.Context, t Topic, env *message.Envelope) error

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, t Topic, env *message.Envelope) error {
	return f(ctx, t, env)
}

// routeKey identifies a handler by topic suffix and method. An empty method
// matches any method for that suffix.
type routeKey struct {
	suffix string
	method string
}

// Router dispatches inbound Cloud API messages to handlers keyed on
// (suffix, method).
type Router struct {
	handlers map[routeKey]Handler
	logger   *slog.Logger
}

// NewRouter creates an empty Router.
func NewRouter(logger *slog.Logger) *Router {
	return &Router{
		handlers: make(map[routeKey]Handler),
		logger:   logger,
	}
}

// Handle registers h for a specific (suffix, method) pair.
func (r *Router) Handle(suffix, method string, h Handler) {
	r.handlers[routeKey{suffix, method}] = h
}

// HandleSuffix registers h for every message with the given suffix regardless
// of method (used for plain osd and for reply correlation).
func (r *Router) HandleSuffix(suffix string, h Handler) {
	r.handlers[routeKey{suffix, ""}] = h
}

// Route parses raw, decodes the envelope and dispatches to the matching
// handler. A message with no registered handler is logged and dropped.
func (r *Router) Route(ctx context.Context, raw string, payload []byte) {
	t, err := Parse(raw)
	if err != nil {
		r.logger.Warn("drop message: bad topic",
			slog.String("topic", raw), slog.String("error", err.Error()))
		return
	}
	env, err := message.Decode(payload)
	if err != nil {
		r.logger.Warn("drop message: bad envelope",
			slog.String("topic", raw), slog.String("error", err.Error()))
		return
	}
	h, ok := r.handlers[routeKey{t.Suffix, env.Method}]
	if !ok {
		h, ok = r.handlers[routeKey{t.Suffix, ""}]
	}
	if !ok {
		r.logger.Debug("no handler for message",
			slog.String("suffix", t.Suffix), slog.String("method", env.Method))
		return
	}
	if err := h.Handle(ctx, t, env); err != nil {
		r.logger.Error("handler error",
			slog.String("suffix", t.Suffix), slog.String("method", env.Method),
			slog.String("error", err.Error()))
	}
}
