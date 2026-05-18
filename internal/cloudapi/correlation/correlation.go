// Package correlation matches Cloud API command replies to their requests by
// transaction id (tid).
package correlation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

// DefaultTimeout is the recommended wait for a command reply (§10).
const DefaultTimeout = 10 * time.Second

// Tracker correlates outbound command replies to requests by tid.
type Tracker struct {
	logger  *slog.Logger
	mu      sync.Mutex
	pending map[string]chan *message.Envelope
}

// New creates an empty Tracker.
func New(logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracker{
		logger:  logger,
		pending: make(map[string]chan *message.Envelope),
	}
}

// Expect registers interest in the reply for tid. It must be called before the
// request is published. The returned cancel func releases the registration and
// must be called (defer it).
func (t *Tracker) Expect(tid string) (<-chan *message.Envelope, func()) {
	ch := make(chan *message.Envelope, 1)
	t.mu.Lock()
	t.pending[tid] = ch
	t.mu.Unlock()
	return ch, func() {
		t.mu.Lock()
		delete(t.pending, tid)
		t.mu.Unlock()
	}
}

// Await registers interest in tid, runs send (which publishes the request),
// and blocks until the reply arrives or ctx is done.
func (t *Tracker) Await(ctx context.Context, tid string, send func() error) (*message.Envelope, error) {
	ch, cancel := t.Expect(tid)
	defer cancel()
	if err := send(); err != nil {
		return nil, err
	}
	select {
	case env := <-ch:
		return env, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("await reply for tid %s: %w", tid, ctx.Err())
	}
}

// Handle implements topic.Handler: it resolves the pending request whose tid
// matches the reply. An unmatched reply — a late or duplicate QoS-1 delivery,
// or a reply this process did not originate — is normal, so it is logged at
// debug and dropped rather than treated as an error.
func (t *Tracker) Handle(_ context.Context, _ topic.Topic, env *message.Envelope) error {
	t.mu.Lock()
	ch, ok := t.pending[env.Tid]
	t.mu.Unlock()
	if !ok {
		t.logger.Debug("dropping unmatched reply", slog.String("tid", env.Tid))
		return nil
	}
	select {
	case ch <- env:
	default:
	}
	return nil
}
