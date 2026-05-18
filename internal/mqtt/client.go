// Package mqtt wraps the autopaho MQTT 5.0 client with the connection,
// subscription and TLS setup the DJI Cloud API bridge needs.
package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/google/uuid"
)

// MessageHandler is invoked for every received PUBLISH.
type MessageHandler func(topic string, payload []byte)

// Client wraps an autopaho ConnectionManager for the DJI Cloud API. Construct
// it with New plus With* options, then call Connect.
type Client struct {
	brokerURL  *url.URL
	clientID   string
	username   string
	password   string
	caCertFile string
	keepAlive  uint16
	subTopics  []string
	handler    MessageHandler
	logger     *slog.Logger

	cm *autopaho.ConnectionManager
}

// Option configures a Client; see the With* constructors.
type Option func(*Client)

// WithClientID sets the MQTT client identifier (defaults to a random UUID).
func WithClientID(id string) Option {
	return func(c *Client) { c.clientID = id }
}

// WithCredentials sets the MQTT username and password.
func WithCredentials(username, password string) Option {
	return func(c *Client) {
		c.username = username
		c.password = password
	}
}

// WithCACert trusts the PEM CA bundle at path for TLS connections; when unset
// the system root store is used.
func WithCACert(path string) Option {
	return func(c *Client) { c.caCertFile = path }
}

// WithKeepAlive sets the MQTT keepalive interval in seconds (defaults to 30).
func WithKeepAlive(seconds uint16) Option {
	return func(c *Client) { c.keepAlive = seconds }
}

// WithSubscriptions sets the topics (re-)subscribed to on every connection.
func WithSubscriptions(topics ...string) Option {
	return func(c *Client) { c.subTopics = topics }
}

// WithMessageHandler sets the callback invoked for each received message.
func WithMessageHandler(h MessageHandler) Option {
	return func(c *Client) { c.handler = h }
}

// WithLogger sets the structured logger (defaults to slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// New creates an MQTT client for the broker at brokerURL (e.g.
// tls://emqx:8883 or mqtt://emqx:1883). Call Connect to dial.
func New(brokerURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(brokerURL)
	if err != nil {
		return nil, fmt.Errorf("parse broker url %q: %w", brokerURL, err)
	}
	c := &Client{
		brokerURL: u,
		clientID:  uuid.NewString(),
		keepAlive: 30,
		handler:   func(string, []byte) {},
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Connect establishes an auto-reconnecting MQTT 5.0 connection. On every
// (re)connection it (re-)subscribes to the configured topics so subscriptions
// survive reconnects.
func (c *Client) Connect(ctx context.Context) error {
	var tlsCfg *tls.Config
	switch c.brokerURL.Scheme {
	case "tls", "ssl", "mqtts":
		var err error
		if tlsCfg, err = buildTLSConfig(c.caCertFile); err != nil {
			return err
		}
	}

	subs := make([]paho.SubscribeOptions, 0, len(c.subTopics))
	for _, t := range c.subTopics {
		subs = append(subs, paho.SubscribeOptions{Topic: t, QoS: 1})
	}

	clientCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{c.brokerURL},
		TlsCfg:                        tlsCfg,
		KeepAlive:                     c.keepAlive,
		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         60,
		ConnectUsername:               c.username,
		ConnectPassword:               []byte(c.password),
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			c.logger.Info("mqtt connected", slog.String("broker", c.brokerURL.String()))
			if len(subs) == 0 {
				return
			}
			// Subscribe off the callback goroutine: autopaho requires that
			// OnConnectionUp does not block.
			go func() {
				if _, err := cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: subs}); err != nil {
					c.logger.Error("mqtt subscribe failed", slog.String("error", err.Error()))
				}
			}()
		},
		OnConnectError: func(err error) {
			c.logger.Warn("mqtt connection error", slog.String("error", err.Error()))
		},
		ClientConfig: paho.ClientConfig{
			ClientID: c.clientID,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					c.handler(pr.Packet.Topic, pr.Packet.Payload)
					return true, nil
				},
			},
			OnClientError: func(err error) {
				c.logger.Warn("mqtt client error", slog.String("error", err.Error()))
			},
		},
	}

	cm, err := autopaho.NewConnection(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("mqtt new connection: %w", err)
	}
	c.cm = cm
	return nil
}

// Publish sends payload on topic at QoS 1.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	if c.cm == nil {
		return fmt.Errorf("mqtt publish %s: not connected", topic)
	}
	if _, err := c.cm.Publish(ctx, &paho.Publish{
		Topic:   topic,
		QoS:     1,
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("mqtt publish %s: %w", topic, err)
	}
	return nil
}

// AwaitConnection blocks until the client is connected or ctx is done.
func (c *Client) AwaitConnection(ctx context.Context) error {
	if c.cm == nil {
		return fmt.Errorf("mqtt: not connected")
	}
	return c.cm.AwaitConnection(ctx)
}

// Disconnect cleanly closes the connection.
func (c *Client) Disconnect(ctx context.Context) error {
	if c.cm == nil {
		return nil
	}
	return c.cm.Disconnect(ctx)
}

func buildTLSConfig(caCertFile string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caCertFile == "" {
		return cfg, nil // trust the system root store
	}
	pem, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("read ca cert %q: %w", caCertFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates found in %q", caCertFile)
	}
	cfg.RootCAs = pool
	return cfg, nil
}
