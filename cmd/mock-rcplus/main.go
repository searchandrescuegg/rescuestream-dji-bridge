// Command mock-rcplus simulates a DJI RC Plus + M30T so the bridge can be
// exercised without real hardware. It onboards through the bridge's HTTP API,
// connects to the broker as the RC Plus gateway, publishes topology and live
// M30T OSD telemetry, and acks inbound service calls.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/telemetry"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/device"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/mqtt"
)

// Device product types (§2): RC Plus gateway = 119, M30T aircraft = 67/1.
const (
	typeRCPlus      = 119
	typeM30T        = 67
	subTypeM30T     = 1
	thingVersionSim = "1.0.0"
)

type mockConfig struct {
	OnboardURL  string        `env:"ONBOARD_URL" envDefault:"http://localhost:8080/api/onboard"`
	APIKey      string        `env:"API_KEY"`
	BrokerURL   string        `env:"BROKER_URL" envDefault:"mqtt://localhost:1883"`
	CACertFile  string        `env:"CA_CERT_FILE"`
	RCSN        string        `env:"RC_SN" envDefault:"RC-MOCK-0001"`
	AircraftSN  string        `env:"AIRCRAFT_SN" envDefault:"M30T-MOCK-0001"`
	OSDInterval time.Duration `env:"OSD_INTERVAL" envDefault:"1s"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("mock-rcplus: %v", err)
	}
}

func run() error {
	var cfg mockConfig
	if err := env.Parse(&cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Onboard through the bridge's HTTP API to obtain MQTT credentials,
	// retrying while the bridge's HTTP server is still coming up.
	username, password, err := onboardWithRetry(ctx, logger, cfg)
	if err != nil {
		return fmt.Errorf("onboard: %w", err)
	}
	logger.Info("onboarded", slog.String("rc_sn", cfg.RCSN))

	// 2. Connect to the broker as the RC Plus gateway. The simulator is built
	// first so its inbound handler can publish replies through the client.
	sim := &simulator{cfg: cfg, logger: logger}
	client, err := mqtt.New(cfg.BrokerURL,
		mqtt.WithClientID("mock-"+cfg.RCSN),
		mqtt.WithCredentials(username, password),
		mqtt.WithCACert(cfg.CACertFile),
		mqtt.WithSubscriptions(
			topic.New(topic.PrefixThing, cfg.RCSN, topic.SuffixServices).String(),
			topic.New(topic.PrefixSys, cfg.RCSN, topic.SuffixStatusReply).String(),
			topic.New(topic.PrefixThing, cfg.RCSN, topic.SuffixDRCDown).String(),
		),
		mqtt.WithMessageHandler(func(t string, payload []byte) {
			sim.handle(ctx, t, payload)
		}),
		mqtt.WithLogger(logger),
	)
	if err != nil {
		return fmt.Errorf("create mqtt client: %w", err)
	}
	sim.client = client

	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("connect mqtt: %w", err)
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(disconnectCtx)
	}()
	if err := client.AwaitConnection(ctx); err != nil {
		return fmt.Errorf("await connection: %w", err)
	}

	// 3. Announce topology, then stream live telemetry until interrupted.
	if err := sim.publishTopology(ctx); err != nil {
		return fmt.Errorf("publish topology: %w", err)
	}
	logger.Info("mock rc plus running", slog.String("aircraft_sn", cfg.AircraftSN))
	sim.runOSDLoop(ctx)
	logger.Info("mock rc plus stopped")
	return nil
}

// simulator holds the runtime state of the mock RC Plus.
type simulator struct {
	cfg    mockConfig
	client *mqtt.Client
	logger *slog.Logger
}

// publishTopology announces the RC Plus and its paired M30T via update_topo.
func (s *simulator) publishTopology(ctx context.Context) error {
	data := map[string]any{
		"type":          typeRCPlus,
		"sub_type":      0,
		"domain":        "3",
		"thing_version": thingVersionSim,
		"device_secret": "mock-gateway-secret",
		"nonce":         "mock-gateway-nonce",
		"sub_devices": []device.SubDevice{{
			SN:           s.cfg.AircraftSN,
			Domain:       "0",
			Type:         typeM30T,
			SubType:      subTypeM30T,
			Index:        "A",
			ThingVersion: thingVersionSim,
			DeviceSecret: "mock-aircraft-secret",
			Nonce:        "mock-aircraft-nonce",
		}},
	}
	t := topic.New(topic.PrefixSys, s.cfg.RCSN, topic.SuffixStatus)
	return s.publish(ctx, t, "update_topo", data)
}

// runOSDLoop publishes simulated M30T OSD telemetry until ctx is cancelled.
func (s *simulator) runOSDLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.OSDInterval)
	defer ticker.Stop()

	state := &flightState{battery: 100}
	osdTopic := topic.New(topic.PrefixThing, s.cfg.AircraftSN, topic.SuffixOSD)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.publish(ctx, osdTopic, "", state.next()); err != nil {
				s.logger.Warn("publish osd failed", slog.String("error", err.Error()))
			}
		}
	}
}

// handle dispatches an inbound message from the bridge.
func (s *simulator) handle(ctx context.Context, rawTopic string, payload []byte) {
	t, err := topic.Parse(rawTopic)
	if err != nil {
		return
	}
	env, err := message.Decode(payload)
	if err != nil {
		return
	}
	switch t.Suffix {
	case topic.SuffixServices:
		s.logger.Info("service call received", slog.String("method", env.Method))
		reply, err := env.Reply(map[string]any{"result": 0})
		if err != nil {
			return
		}
		reply.Gateway = s.cfg.RCSN
		out, err := reply.Encode()
		if err != nil {
			return
		}
		replyTopic := topic.New(topic.PrefixThing, s.cfg.RCSN, topic.SuffixServicesReply)
		if err := s.client.Publish(ctx, replyTopic.String(), out); err != nil {
			s.logger.Warn("publish services_reply failed", slog.String("error", err.Error()))
		}
	case topic.SuffixStatusReply:
		s.logger.Info("topology acknowledged by bridge")
	case topic.SuffixDRCDown:
		s.logger.Info("drc downlink received", slog.String("method", env.Method))
	}
}

// publish wraps data in a Cloud API envelope and publishes it.
func (s *simulator) publish(ctx context.Context, t topic.Topic, method string, data any) error {
	env, err := message.New(method, data)
	if err != nil {
		return err
	}
	env.Gateway = s.cfg.RCSN
	payload, err := env.Encode()
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, t.String(), payload)
}

// flightState produces a plausible evolving M30T OSD: a slow circular flight
// over Seattle with a gradually draining battery.
type flightState struct {
	tick    int
	battery int
}

func (f *flightState) next() telemetry.AircraftOSD {
	f.tick++
	if f.tick%20 == 0 && f.battery > 10 {
		f.battery--
	}
	const baseLat, baseLon = 47.6062, -122.3321
	angle := float64(f.tick) * 0.1
	return telemetry.AircraftOSD{
		ModeCode:        telemetry.ModeManualFlight,
		Latitude:        baseLat + 0.001*math.Sin(angle),
		Longitude:       baseLon + 0.001*math.Cos(angle),
		Height:          50 + 10*math.Sin(angle),
		Elevation:       50,
		AttitudeHead:    math.Mod(angle*57.2958, 360),
		HorizontalSpeed: 5.0,
		VerticalSpeed:   math.Cos(angle),
		HomeDistance:    20 + 5*math.Sin(angle),
		HomeLatitude:    baseLat,
		HomeLongitude:   baseLon,
		WindSpeed:       3.2,
		WindDirection:   3,
		PositionState:   telemetry.PositionState{IsFixed: 2, Quality: 5, GPSNumber: 16, RTKNumber: 20},
		Battery:         telemetry.Battery{CapacityPercent: f.battery, RemainFlightTime: f.battery * 18},
		ControlSource:   "A",
	}
}

// onboardWithRetry calls onboard, retrying for up to ~45s while the bridge's
// HTTP server is still starting.
func onboardWithRetry(ctx context.Context, logger *slog.Logger, cfg mockConfig) (username, password string, err error) {
	for attempt := 1; attempt <= 45; attempt++ {
		username, password, err = onboard(ctx, cfg.OnboardURL, cfg.APIKey, cfg.RCSN, cfg.AircraftSN)
		if err == nil {
			return username, password, nil
		}
		logger.Warn("onboard attempt failed, retrying",
			slog.Int("attempt", attempt), slog.String("error", err.Error()))
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", "", err
}

// onboard calls the bridge's HTTP onboarding endpoint and returns the MQTT
// username and password issued for the RC Plus.
func onboard(ctx context.Context, url, apiKey, rcSN, aircraftSN string) (username, password string, err error) {
	reqBody, err := json.Marshal(map[string]string{"rc_sn": rcSN, "aircraft_sn": aircraftSN})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("onboarding endpoint returned %s", resp.Status)
	}

	var r struct {
		Host     string `json:"host"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", fmt.Errorf("decode onboard response: %w", err)
	}
	return r.Username, r.Password, nil
}
