package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	MetricsEnabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	MetricsPort    int  `env:"METRICS_PORT" envDefault:"8081"`

	Local bool `env:"LOCAL" envDefault:"false"`

	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"false"`
	TracingSampleRate float64 `env:"TRACING_SAMPLERATE" envDefault:"0.01"`
	TracingService    string  `env:"TRACING_SERVICE" envDefault:"rescuestream-dji-bridge"`
	TracingVersion    string  `env:"TRACING_VERSION"`

	// HTTPPort is the port the onboarding HTTP server listens on.
	HTTPPort int `env:"HTTP_PORT" envDefault:"8080"`

	// MQTT — MQTTBrokerURL is the broker the backend connects to (autopaho
	// scheme: mqtt:// or tls://). MQTTPublicHost is the host string returned
	// to devices during onboarding; the RC Plus / JSBridge requires a tcp://
	// or ws:// scheme on it (§5.1).
	MQTTBrokerURL  string `env:"MQTT_BROKER_URL" envDefault:"mqtt://localhost:1883"`
	MQTTPublicHost string `env:"MQTT_PUBLIC_HOST" envDefault:"tcp://localhost:1883"`
	MQTTUsername   string `env:"MQTT_USERNAME"`
	MQTTPassword   string `env:"MQTT_PASSWORD"`
	MQTTCACertFile string `env:"MQTT_CA_CERT_FILE"`

	// Per-device JWT credentials minted for the RC Plus.
	JWTPrivateKeyFile string        `env:"JWT_PRIVATE_KEY_FILE"`
	JWTTTL            time.Duration `env:"JWT_TTL" envDefault:"1h"`

	// Telemetry persistence — Postgres/Neon connection string. When set, all
	// downlinked telemetry is archived for playback; empty runs in-memory-only.
	DatabaseURL string `env:"DATABASE_URL"`

	// DJI developer credentials — injected into the onboarding WebView page and
	// returned in the MQTT `config` reply, which the device's cloud module uses
	// to verify the Cloud API license (gates the whole connection).
	DJIAppID      string `env:"DJI_APP_ID"`
	DJIAppKey     string `env:"DJI_APP_KEY"`
	DJILicense    string `env:"DJI_LICENSE"`
	NTPServerHost string `env:"NTP_SERVER_HOST"`
	PlatformName  string `env:"PLATFORM_NAME" envDefault:"RescueStream"`
	WorkspaceID   string `env:"WORKSPACE_ID"`

	// HTTP API authentication. When APIKey is empty the server runs open with
	// a startup warning (dev only). SNAllowlist, when non-empty, restricts
	// which RC Plus serial numbers may be onboarded.
	APIKey      string   `env:"API_KEY"`
	SNAllowlist []string `env:"SN_ALLOWLIST"`
}

func NewConfig() (*Config, error) {
	var cfg Config

	err := env.Parse(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}
