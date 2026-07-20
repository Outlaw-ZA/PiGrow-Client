package mqtt

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

// MessageHandler is the callback signature for incoming MQTT messages.
type MessageHandler func(topic string, payload []byte)

// Client is the interface the controller uses for MQTT operations,
// allowing test injection without a real broker.
type Client interface {
	Publish(topic string, payload []byte) error
	PublishQoS0(topic string, payload []byte) error
	Subscribe(topic string, handler MessageHandler) error
	Disconnect()
}

// subEntry stores a subscription for replay on reconnect.
type subEntry struct {
	topic   string
	handler MessageHandler
}

// mqttClient wraps the paho MQTT client.
type mqttClient struct {
	client mqttlib.Client
	subs   []subEntry // stored for reconnect replay
	mu     sync.Mutex
}

// compile-time check that *mqttClient satisfies Client.
var _ Client = (*mqttClient)(nil)

// Config holds the parameters needed to connect.
type Config struct {
	Broker         string
	ClientID       string
	ConnectTimeout time.Duration
	KeepAlive      time.Duration
	Username       string
	Password       string
	StatusTopic    string // retained online/offline will topic; empty = no will
}

// New creates a Client and connects to the broker.
func New(cfg Config) (Client, error) {
	c := &mqttClient{}

	opts := mqttlib.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetKeepAlive(cfg.KeepAlive).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetOrderMatters(false).
		SetCleanSession(false)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	// Register MQTT Last Will & Testament: publish retained offline status
	// payload on unexpected disconnect so the server detects the Pi going
	// silent within one keepalive interval.
	if cfg.StatusTopic != "" {
		offlinePayload := `{"online":false}`
		opts.SetWill(cfg.StatusTopic, offlinePayload, 1, true)
	}

	opts.OnConnectionLost = func(c mqttlib.Client, err error) {
		slog.Error("MQTT connection lost", "error", err)
	}
	opts.OnReconnecting = func(c mqttlib.Client, opts *mqttlib.ClientOptions) {
		slog.Info("MQTT reconnecting")
	}
	opts.OnConnect = func(client mqttlib.Client) {
		slog.Info("MQTT connected", "broker", cfg.Broker)
		// Publish retained online status so anyone subscribed sees
		// real-time connectivity rather than stale cached data.
		if cfg.StatusTopic != "" {
			onlinePayload := `{"online":true}`
			token := client.Publish(cfg.StatusTopic, 1, true, onlinePayload)
			token.Wait()
		}
		c.replaySubscriptions(client)
	}

	client := mqttlib.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(cfg.ConnectTimeout) {
		return nil, fmt.Errorf("mqtt connect timed out after %v", cfg.ConnectTimeout)
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("mqtt connect: %w", err)
	}

	c.client = client
	return c, nil
}

// Publish sends a message to the given topic with QoS 1.
func (c *mqttClient) Publish(topic string, payload []byte) error {
	token := c.client.Publish(topic, 1, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

// PublishQoS0 sends a fire-and-forget message at QoS 0 — suitable for
// periodic telemetry where a dropped sample is acceptable.
func (c *mqttClient) PublishQoS0(topic string, payload []byte) error {
	c.client.Publish(topic, 0, false, payload)
	return nil
}

// Subscribe registers a handler for a topic filter and stores it for
// replay on reconnects.
func (c *mqttClient) Subscribe(topic string, handler MessageHandler) error {
	token := c.client.Subscribe(topic, 1, func(_ mqttlib.Client, msg mqttlib.Message) {
		handler(msg.Topic(), msg.Payload())
	})
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("subscribe to %s: timed out after 5s", topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("subscribe to %s: %w", topic, err)
	}
	c.mu.Lock()
	c.subs = append(c.subs, subEntry{topic: topic, handler: handler})
	c.mu.Unlock()
	slog.Info("MQTT subscribed", "topic", topic)
	return nil
}

// replaySubscriptions re-subscribes all stored topic/handler pairs — used
// inside OnConnect so subscriptions survive broker reconnects.
func (c *mqttClient) replaySubscriptions(client mqttlib.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.subs {
		token := client.Subscribe(s.topic, 1, func(_ mqttlib.Client, msg mqttlib.Message) {
			s.handler(msg.Topic(), msg.Payload())
		})
		if !token.WaitTimeout(5 * time.Second) {
			slog.Warn("MQTT re-subscribe timed out", "topic", s.topic)
			continue
		}
		if err := token.Error(); err != nil {
			slog.Error("MQTT re-subscribe failed", "topic", s.topic, "error", err)
			continue
		}
		slog.Info("MQTT re-subscribed", "topic", s.topic)
	}
}

// Disconnect cleanly closes the connection (with a timeout so it never hangs).
func (c *mqttClient) Disconnect() {
	c.client.Disconnect(250)
}
