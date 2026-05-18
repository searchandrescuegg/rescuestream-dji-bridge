// Package live implements the P2 live-streaming service calls (§7). The bridge
// publishes these as `services` requests to the RC Plus gateway and correlates
// the gateway's reply by tid.
//
// P2 scaffold: the service calls below are functional, but no caller triggers
// them yet — wiring an HTTP endpoint that starts/stops streams is P2's
// remaining work. Verify payload schemas against
// docs/.../10.pilot-to-cloud/00.mqtt/20.rc-pro/20.live.md before relying on it.
package live

import (
	"context"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/correlation"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

// Live-streaming service method names (§7).
const (
	MethodStartPush  = "live_start_push"
	MethodStopPush   = "live_stop_push"
	MethodSetQuality = "live_set_quality"
	MethodLensChange = "live_lens_change"
)

// URL types for live_start_push (§7.1).
const (
	URLTypeAgora   = 0
	URLTypeRTMP    = 1
	URLTypeGB28181 = 3
)

// Video quality levels for live_start_push / live_set_quality (§7.1).
const (
	QualityAdaptive = 0
	QualitySmooth   = 1
	QualitySD       = 2
	QualityHD       = 3
	QualityUHD      = 4
)

// StartPushRequest is the data payload of a live_start_push call (§7.1).
// VideoID is "{sn}/{camera_index}/{video_index}" — the M30T main camera is
// camera index 53-0-0.
type StartPushRequest struct {
	URLType      int    `json:"url_type"`
	URL          string `json:"url"`
	VideoID      string `json:"video_id"`
	VideoQuality int    `json:"video_quality"`
}

// StopPushRequest is the data payload of a live_stop_push call.
type StopPushRequest struct {
	VideoID string `json:"video_id"`
}

// SetQualityRequest is the data payload of a live_set_quality call.
type SetQualityRequest struct {
	VideoID      string `json:"video_id"`
	VideoQuality int    `json:"video_quality"`
}

// LensChangeRequest is the data payload of a live_lens_change call. VideoType
// is one of "normal", "zoom", "wide", "thermal" (§7).
type LensChangeRequest struct {
	VideoID   string `json:"video_id"`
	VideoType string `json:"video_type"`
}

// Publisher publishes a payload to an MQTT topic.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Service issues live-streaming service calls to RC Plus gateways.
type Service struct {
	pub     Publisher
	tracker *correlation.Tracker
}

// NewService creates a live-streaming Service.
func NewService(pub Publisher, tracker *correlation.Tracker) *Service {
	return &Service{pub: pub, tracker: tracker}
}

// StartPush starts a live stream from a camera and waits for the gateway reply.
func (s *Service) StartPush(ctx context.Context, gatewaySN string, req StartPushRequest) (*message.Envelope, error) {
	return s.call(ctx, gatewaySN, MethodStartPush, req)
}

// StopPush stops a running live stream.
func (s *Service) StopPush(ctx context.Context, gatewaySN string, req StopPushRequest) (*message.Envelope, error) {
	return s.call(ctx, gatewaySN, MethodStopPush, req)
}

// SetQuality changes the quality of a running live stream.
func (s *Service) SetQuality(ctx context.Context, gatewaySN string, req SetQualityRequest) (*message.Envelope, error) {
	return s.call(ctx, gatewaySN, MethodSetQuality, req)
}

// LensChange switches the lens of a running live stream.
func (s *Service) LensChange(ctx context.Context, gatewaySN string, req LensChangeRequest) (*message.Envelope, error) {
	return s.call(ctx, gatewaySN, MethodLensChange, req)
}

// call publishes a service request to the gateway and awaits its reply.
func (s *Service) call(ctx context.Context, gatewaySN, method string, data any) (*message.Envelope, error) {
	env, err := message.New(method, data)
	if err != nil {
		return nil, err
	}
	env.Gateway = gatewaySN
	payload, err := env.Encode()
	if err != nil {
		return nil, err
	}
	return s.tracker.Await(ctx, env.Tid, func() error {
		return s.pub.Publish(ctx, topic.Services(gatewaySN).String(), payload)
	})
}
