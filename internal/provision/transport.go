package provision

import (
	"fmt"
	"time"

	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

// PahoTransport wraps a paho MQTT client so it satisfies ClaimTransport.
// It owns no connection state of its own — the caller is expected to
// have already configured and connected the underlying client (this is
// the same shape internal/mqtt uses for the active loop).
type PahoTransport struct {
	Client mqttlib.Client
}

// Subscribe registers a handler for the given claim topic.
func (p *PahoTransport) Subscribe(topic string, h ClaimHandler) error {
	tok := p.Client.Subscribe(topic, 1, func(_ mqttlib.Client, msg mqttlib.Message) {
		h(msg.Payload())
	})
	if !tok.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("subscribe %s: timed out after 5s", topic)
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("subscribe %s: %w", topic, err)
	}
	return nil
}

// Unsubscribe tears down the subscription; the paho Unsubscribe is
// fire-and-forget over the wire, so we wait briefly and surface any
// error.
func (p *PahoTransport) Unsubscribe(topic string) error {
	tok := p.Client.Unsubscribe(topic)
	if !tok.WaitTimeout(2 * time.Second) {
		return fmt.Errorf("unsubscribe %s: timed out after 2s", topic)
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("unsubscribe %s: %w", topic, err)
	}
	return nil
}

// Connected reports whether the underlying paho client believes the
// broker is reachable. Useful for guard checks before Subscribe.
func (p *PahoTransport) Connected() bool { return p.Client.IsConnected() }

// Close is a no-op on this transport — the underlying client lifecycle
// is owned by the caller. Kept here so ClaimTransport users can rely
// on a uniform shape.
func (p *PahoTransport) Close() error { return nil }

// DialClaimTransport opens a stand-alone paho MQTT client suitable for
// the provisioning claim handshake. It uses anonymous auth in v1
// (broker keeps allow_anonymous true until Phase 2).
func DialClaimTransport(broker, clientID string) (*PahoTransport, error) {
	opts := mqttlib.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID + "-provision").
		SetConnectTimeout(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetCleanSession(true)

	client := mqttlib.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(5 * time.Second) {
		return nil, fmt.Errorf("connect %s: timed out", broker)
	}
	if err := tok.Error(); err != nil {
		return nil, fmt.Errorf("connect %s: %w", broker, err)
	}
	return &PahoTransport{Client: client}, nil
}
