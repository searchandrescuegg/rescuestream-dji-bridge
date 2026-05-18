package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

// methodConfig is the device request that carries Cloud API license verification.
const methodConfig = "config"

// DJICredentials are the DJI developer-platform values the device's cloud
// module re-verifies over MQTT via the `config` request.
type DJICredentials struct {
	AppID         string
	AppKey        string
	License       string
	NTPServerHost string
}

// Requests handles thing/product/{sn}/requests — device-initiated requests.
// The onboarding-critical one is `config`: the RC Plus's cloud module verifies
// the Cloud API license against the reply and stalls the entire connection
// (no update_topo, no osd, connectCallback failure) until it gets a valid one.
type Requests struct {
	creds  DJICredentials
	pub    Publisher
	logger *slog.Logger
}

// NewRequests creates a device-requests handler.
func NewRequests(creds DJICredentials, pub Publisher, logger *slog.Logger) *Requests {
	return &Requests{creds: creds, pub: pub, logger: logger}
}

// Handle implements topic.Handler.
func (h *Requests) Handle(ctx context.Context, t topic.Topic, env *message.Envelope) error {
	switch env.Method {
	case methodConfig:
		return h.handleConfig(ctx, t, env)
	default:
		h.logger.Info("unhandled device request",
			slog.String("sn", t.SN), slog.String("method", env.Method))
		return nil
	}
}

// handleConfig answers the device's config request with the Cloud API
// developer credentials; the device's license verification gates onboarding.
// The config reply carries its fields flat in data (not wrapped in output).
func (h *Requests) handleConfig(ctx context.Context, t topic.Topic, env *message.Envelope) error {
	data := map[string]any{
		"result":      0,
		"app_id":      h.creds.AppID,
		"app_key":     h.creds.AppKey,
		"app_license": h.creds.License,
	}
	if h.creds.NTPServerHost != "" {
		data["ntp_server_host"] = h.creds.NTPServerHost
	}

	reply, err := env.Reply(data)
	if err != nil {
		return fmt.Errorf("config reply: %w", err)
	}
	payload, err := reply.Encode()
	if err != nil {
		return fmt.Errorf("config reply: %w", err)
	}
	h.logger.Info("answered config request (license verification)", slog.String("sn", t.SN))
	return h.pub.Publish(ctx, topic.RequestsReply(t.SN).String(), payload)
}
