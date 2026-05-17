package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"alpineworks.io/ootel"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/archive"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/correlation"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/config"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/device"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/handler"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/logging"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/mqtt"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/onboarding"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("rescuestream-dji-bridge: %v", err)
	}
}

func run() error {
	c, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := validatePublicHost(c.MQTTPublicHost); err != nil {
		return err
	}

	slogLevel, err := logging.LogLevelToSlogLevel(c.LogLevel)
	if err != nil {
		return fmt.Errorf("parse log level: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// msgCtx scopes inbound message handling. It is deliberately NOT the
	// signal context, so handlers can still publish replies during the
	// shutdown window; it is cancelled last, after the MQTT disconnect.
	msgCtx, msgCancel := context.WithCancel(context.Background())
	defer msgCancel()

	shutdownObservability, err := initObservability(ctx, c)
	if err != nil {
		return err
	}
	defer shutdownObservability()

	// Bridge components: registry of devices, the latest-telemetry store, the
	// topic router, and the correlation tracker for command request/reply.
	registry := device.NewRegistry()
	store := device.NewStore()
	tracker := correlation.New(logger)
	router := topic.NewRouter(logger)

	// Telemetry archive (optional) — persists all downlinked telemetry to
	// Postgres for playback. Created before the MQTT client so its deferred
	// Close runs after the MQTT disconnect (no telemetry enqueued mid-flush).
	var archiveWriter *archive.Writer
	if c.DatabaseURL != "" {
		archiveWriter = archive.New(c.DatabaseURL, archive.WithLogger(logger))
		if err := archiveWriter.Connect(ctx); err != nil {
			return fmt.Errorf("connect telemetry archive: %w", err)
		}
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			archiveWriter.Close(closeCtx)
		}()
		logger.Info("telemetry archive connected")
	} else {
		logger.Warn("DATABASE_URL not set — telemetry persistence disabled")
	}

	// MQTT client — built before handlers so handlers can publish replies
	// through it; it routes every inbound message to the router.
	mqttClient, err := mqtt.New(c.MQTTBrokerURL,
		mqtt.WithClientID("rescuestream-dji-bridge"),
		mqtt.WithCredentials(c.MQTTUsername, c.MQTTPassword),
		mqtt.WithCACert(c.MQTTCACertFile),
		mqtt.WithSubscriptions(topic.SubscriptionTopics()...),
		mqtt.WithMessageHandler(func(t string, payload []byte) {
			router.Route(msgCtx, t, payload)
		}),
		mqtt.WithLogger(logger),
	)
	if err != nil {
		return fmt.Errorf("create mqtt client: %w", err)
	}

	// Handlers — P0 topology + P1 telemetry; command replies route to the
	// correlation tracker.
	router.Handle(topic.SuffixStatus, "update_topo", handler.NewTopology(registry, mqttClient, archiveWriter, logger))
	router.HandleSuffix(topic.SuffixRequests, handler.NewRequests(handler.DJICredentials{
		AppID:         c.DJIAppID,
		AppKey:        c.DJIAppKey,
		License:       c.DJILicense,
		NTPServerHost: c.NTPServerHost,
	}, mqttClient, logger))
	router.HandleSuffix(topic.SuffixOSD, handler.NewOSD(registry, store, archiveWriter, logger))
	router.HandleSuffix(topic.SuffixState, handler.NewState(registry, store, mqttClient, archiveWriter, logger))
	router.HandleSuffix(topic.SuffixEvents, handler.NewEvents(mqttClient, archiveWriter, logger))
	router.HandleSuffix(topic.SuffixDRCUp, handler.NewDRC(logger))
	router.HandleSuffix(topic.SuffixServicesReply, tracker)

	if err := mqttClient.Connect(ctx); err != nil {
		return fmt.Errorf("connect mqtt: %w", err)
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mqttClient.Disconnect(disconnectCtx)
	}()

	server, err := newOnboardingServer(c, registry, store, logger)
	if err != nil {
		return err
	}

	logger.Info("rescuestream-dji-bridge started", slog.Int("http_port", c.HTTPPort))
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("onboarding server: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}

// validatePublicHost ensures MQTT_PUBLIC_HOST uses a scheme the RC Plus
// JSBridge cloud module accepts (§5.1: tcp:// or ws://).
func validatePublicHost(host string) error {
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("invalid MQTT_PUBLIC_HOST %q: %w", host, err)
	}
	switch u.Scheme {
	case "tcp", "ws", "wss":
		return nil
	default:
		return fmt.Errorf("MQTT_PUBLIC_HOST %q: scheme must be tcp:// or ws:// (RC Plus JSBridge requirement)", host)
	}
}

// initObservability wires up ootel metrics/tracing and runtime/host
// instrumentation, returning a shutdown func.
func initObservability(ctx context.Context, c *config.Config) (func(), error) {
	exporterType := ootel.ExporterTypePrometheus
	if c.Local {
		exporterType = ootel.ExporterTypeOTLPGRPC
	}
	ootelClient := ootel.NewOotelClient(
		ootel.WithMetricConfig(
			ootel.NewMetricConfig(c.MetricsEnabled, exporterType, c.MetricsPort),
		),
		ootel.WithTraceConfig(
			ootel.NewTraceConfig(c.TracingEnabled, c.TracingSampleRate, c.TracingService, c.TracingVersion),
		),
	)
	shutdown, err := ootelClient.Init(ctx)
	if err != nil {
		return nil, fmt.Errorf("init observability: %w", err)
	}
	if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("start runtime metrics: %w", err)
	}
	if err := host.Start(); err != nil {
		return nil, fmt.Errorf("start host metrics: %w", err)
	}
	return func() { _ = shutdown(context.Background()) }, nil
}

// newOnboardingServer builds the HTTP onboarding server, loading the JWT
// minter when a signing key is configured.
func newOnboardingServer(
	c *config.Config,
	registry *device.Registry,
	store *device.Store,
	logger *slog.Logger,
) (*onboarding.Server, error) {
	var minter *onboarding.Minter
	if c.JWTPrivateKeyFile != "" {
		warnIfKeyWorldReadable(c.JWTPrivateKeyFile, logger)
		m, err := onboarding.NewMinter(c.JWTPrivateKeyFile, c.JWTTTL)
		if err != nil {
			return nil, fmt.Errorf("load jwt minter: %w", err)
		}
		minter = m
	} else {
		logger.Warn("JWT_PRIVATE_KEY_FILE not set — onboarding credential issuance disabled")
	}

	if c.APIKey == "" {
		logger.Warn("API_KEY not set — HTTP endpoints are UNAUTHENTICATED (development only)")
	}

	server, err := onboarding.New(fmt.Sprintf(":%d", c.HTTPPort),
		onboarding.WithMinter(minter),
		onboarding.WithRegistry(registry),
		onboarding.WithDeviceStore(store),
		onboarding.WithPublicMQTTHost(c.MQTTPublicHost),
		onboarding.WithAPIKey(c.APIKey),
		onboarding.WithSNAllowlist(c.SNAllowlist),
		onboarding.WithPageConfig(onboarding.PageConfig{
			AppID:        c.DJIAppID,
			AppKey:       c.DJIAppKey,
			License:      c.DJILicense,
			PlatformName: c.PlatformName,
			WorkspaceID:  c.WorkspaceID,
		}),
		onboarding.WithLogger(logger),
	)
	if err != nil {
		return nil, fmt.Errorf("create onboarding server: %w", err)
	}
	return server, nil
}

// warnIfKeyWorldReadable logs a warning when the JWT signing key file is
// accessible beyond its owner.
func warnIfKeyWorldReadable(path string, logger *slog.Logger) {
	info, err := os.Stat(path)
	if err != nil {
		return // NewMinter will surface the read error
	}
	if info.Mode().Perm()&0o077 != 0 {
		logger.Warn("jwt private key file is group/world-accessible — restrict it to 0600",
			slog.String("file", path),
			slog.String("mode", info.Mode().Perm().String()))
	}
}
