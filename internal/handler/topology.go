// Package handler contains the topic handlers that process inbound Cloud API
// messages and update bridge state.
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

// Publisher publishes a payload to an MQTT topic.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// topologyData is the data field of an update_topo message.
type topologyData struct {
	Type         int                `json:"type"`
	SubType      int                `json:"sub_type"`
	Domain       string             `json:"domain"`
	ThingVersion string             `json:"thing_version"`
	DeviceSecret string             `json:"device_secret"`
	Nonce        string             `json:"nonce"`
	SubDevices   []device.SubDevice `json:"sub_devices"`
}

// Topology handles sys/product/{rc}/status update_topo messages: it records the
// RC Plus <-> aircraft mapping, archives it, and acks on status_reply (§5.3).
type Topology struct {
	registry *device.Registry
	pub      Publisher
	archive  *archive.Writer
	logger   *slog.Logger
}

// NewTopology creates a topology handler. arc may be nil (persistence off).
func NewTopology(registry *device.Registry, pub Publisher, arc *archive.Writer, logger *slog.Logger) *Topology {
	return &Topology{registry: registry, pub: pub, archive: arc, logger: logger}
}

// Handle implements topic.Handler.
func (h *Topology) Handle(ctx context.Context, t topic.Topic, env *message.Envelope) error {
	var data topologyData
	if err := env.DecodeData(&data); err != nil {
		return fmt.Errorf("topology: %w", err)
	}
	h.registry.ApplyTopology(t.SN, data.Type, data.SubType, data.SubDevices)

	online := len(data.SubDevices) > 0
	aircraftSN := ""
	if online {
		aircraftSN = data.SubDevices[0].SN
		h.logger.Info("topology updated",
			slog.String("rc_sn", t.SN), slog.String("aircraft_sn", aircraftSN))
	} else {
		h.logger.Info("topology updated: aircraft offline", slog.String("rc_sn", t.SN))
	}
	if h.archive != nil {
		h.archive.Topology(t.SN, aircraftSN, online, env.Timestamp, env.Data)
	}

	reply, err := env.Reply(map[string]any{"result": 0})
	if err != nil {
		return fmt.Errorf("topology reply: %w", err)
	}
	payload, err := reply.Encode()
	if err != nil {
		return fmt.Errorf("topology reply: %w", err)
	}
	return h.pub.Publish(ctx, topic.StatusReply(t.SN).String(), payload)
}
