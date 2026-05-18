// Package topic parses and builds DJI Cloud API MQTT topics and routes
// inbound messages to handlers.
package topic

import (
	"fmt"
	"strings"
)

// Cloud API topic prefixes.
const (
	PrefixThing = "thing"
	PrefixSys   = "sys"
)

// Cloud API topic suffixes (the segment(s) after the serial number).
const (
	SuffixOSD           = "osd"
	SuffixState         = "state"
	SuffixStateReply    = "state_reply"
	SuffixEvents        = "events"
	SuffixEventsReply   = "events_reply"
	SuffixStatus        = "status"
	SuffixStatusReply   = "status_reply"
	SuffixServices      = "services"
	SuffixServicesReply = "services_reply"
	SuffixRequests      = "requests"
	SuffixRequestsReply = "requests_reply"
	SuffixDRCUp         = "drc/up"
	SuffixDRCDown       = "drc/down"
)

// Topic is a parsed Cloud API MQTT topic of the form
// {prefix}/product/{sn}/{suffix}.
type Topic struct {
	Prefix string // PrefixThing or PrefixSys
	SN     string // device or gateway serial number
	Suffix string // e.g. SuffixOSD, SuffixServices, "drc/up"
}

// New builds a Topic from its components.
func New(prefix, sn, suffix string) Topic {
	return Topic{Prefix: prefix, SN: sn, Suffix: suffix}
}

// Parse splits a raw MQTT topic into its components.
func Parse(raw string) (Topic, error) {
	parts := strings.Split(raw, "/")
	if len(parts) < 4 || parts[1] != "product" {
		return Topic{}, fmt.Errorf("not a cloud api topic: %q", raw)
	}
	return Topic{
		Prefix: parts[0],
		SN:     parts[2],
		Suffix: strings.Join(parts[3:], "/"),
	}, nil
}

// String renders the topic to its wire form.
func (t Topic) String() string {
	return fmt.Sprintf("%s/product/%s/%s", t.Prefix, t.SN, t.Suffix)
}

// IsReply reports whether the topic carries a reply to a request we sent.
func (t Topic) IsReply() bool {
	return strings.HasSuffix(t.Suffix, "_reply")
}

// Services returns the topic service calls are published to for a gateway.
func Services(gatewaySN string) Topic { return New(PrefixThing, gatewaySN, SuffixServices) }

// StatusReply returns the topic used to reply to a gateway status message.
func StatusReply(gatewaySN string) Topic { return New(PrefixSys, gatewaySN, SuffixStatusReply) }

// EventsReply returns the topic used to acknowledge a device event that has
// need_reply set.
func EventsReply(gatewaySN string) Topic { return New(PrefixThing, gatewaySN, SuffixEventsReply) }

// StateReply returns the topic used to acknowledge a device state message.
func StateReply(deviceSN string) Topic { return New(PrefixThing, deviceSN, SuffixStateReply) }

// RequestsReply returns the topic used to answer a device requests message.
func RequestsReply(deviceSN string) Topic { return New(PrefixThing, deviceSN, SuffixRequestsReply) }

// DRCDown returns the downlink DRC topic for a gateway.
func DRCDown(gatewaySN string) Topic { return New(PrefixThing, gatewaySN, SuffixDRCDown) }

// SubscriptionTopics are the wildcard topics the backend subscribes to in
// order to receive all device telemetry, events and replies.
func SubscriptionTopics() []string {
	return []string{
		PrefixThing + "/product/+/" + SuffixOSD,
		PrefixThing + "/product/+/" + SuffixState,
		PrefixThing + "/product/+/" + SuffixEvents,
		PrefixThing + "/product/+/" + SuffixServicesReply,
		PrefixThing + "/product/+/" + SuffixRequests,
		PrefixThing + "/product/+/" + SuffixDRCUp,
		PrefixSys + "/product/+/" + SuffixStatus,
	}
}
