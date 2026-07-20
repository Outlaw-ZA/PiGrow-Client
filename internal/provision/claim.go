package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// ClaimResponse is the §3.2 server-to-Pi payload.
type ClaimResponse struct {
	Schema        int    `json:"schema"`
	ControllerID  string `json:"controllerId"`
	ControllerMAC string `json:"controllerMac"`
	MQTTBrokerURL string `json:"mqttBrokerUrl"`
	MQTTUsername  string `json:"mqttUsername"`
	MQTTPassword  string `json:"mqttPassword"`
	ServerHTTPURL string `json:"serverHttpUrl"`
	PairedAt      int64  `json:"pairedAt"`
}

// Verify checks the §3.3 Pi-side invariants on a parsed ClaimResponse:
// schema must be 1, controllerMac must match the Pi's own MAC.
func (r *ClaimResponse) Verify(ownMAC string) error {
	if r == nil {
		return fmt.Errorf("nil claim response")
	}
	if r.Schema != 1 {
		return fmt.Errorf("unsupported schema %d (want 1)", r.Schema)
	}
	if r.ControllerMAC == "" {
		return fmt.Errorf("missing controllerMac")
	}
	want, err := FormatMACString(ownMAC)
	if err != nil {
		return fmt.Errorf("parse own MAC: %w", err)
	}
	wantResp, err := FormatMACString(r.ControllerMAC)
	if err != nil {
		return fmt.Errorf("parse claimed MAC %q: %w", r.ControllerMAC, err)
	}
	if want != wantResp {
		return fmt.Errorf("controllerMac mismatch: own=%s got=%s", want, wantResp)
	}
	if r.ControllerID == "" {
		return fmt.Errorf("missing controllerId")
	}
	if r.ServerHTTPURL == "" {
		return fmt.Errorf("missing serverHttpUrl")
	}
	return nil
}

// ToActiveState converts a verified claim into the on-disk state.json
// representation (spec §3.3 step 2).
func (r *ClaimResponse) ToActiveState() *ActiveState {
	return &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   r.ControllerID,
		ControllerMAC:  FormatMACStringOr(r.ControllerMAC),
		MQTTBrokerURL:  r.MQTTBrokerURL,
		MQTTUsername:   r.MQTTUsername,
		MQTTPassword:   r.MQTTPassword,
		ServerHTTPURL:  r.ServerHTTPURL,
		PairedAt:       r.PairedAt,
	}
}

// FormatMACStringOr returns the colon-separated uppercase form, or the
// input unchanged if the parse fails — better to persist what the
// server sent than to corrupt the state.
func FormatMACStringOr(s string) string {
	if out, err := FormatMACString(s); err == nil {
		return out
	}
	return s
}

// ClaimHandler is the function the MQTT library invokes for incoming
// claim messages. It's the smallest seam between our subscription
// concerns and the paho (or mock) transport.
type ClaimHandler func(payload []byte)

// ClaimTransport is the minimal MQTT surface the claim-waiter needs.
// Both real paho clients and test fakes can satisfy it.
type ClaimTransport interface {
	Subscribe(topic string, handler ClaimHandler) error
	Unsubscribe(topic string) error
	Connected() bool
}

// ClaimSubscriber holds the parameters for waiting on a ClaimResponse.
type ClaimSubscriber struct {
	Transport  ClaimTransport
	OwnMAC     string
	StatePath  string
}

// NewClaimSubscriber is a thin constructor for symmetry with other
// provisioning types; trivial inputs make the explicit form clearer.
func NewClaimSubscriber(t ClaimTransport, ownMAC, statePath string) *ClaimSubscriber {
	return &ClaimSubscriber{
		Transport: t,
		OwnMAC:    ownMAC,
		StatePath: statePath,
	}
}

// ClaimTopic returns the spec §3.1 topic for a colon-separated upper MAC.
func ClaimTopic(mac string) string {
	norm, err := FormatMACString(mac)
	if err != nil {
		norm = mac
	}
	return "provision/" + norm + "/claim"
}

// Wait blocks until a valid ClaimResponse is received on
// provision/<ownMAC>/claim or ctx is cancelled. On success it
// persists state.json and returns the active state.
func (c *ClaimSubscriber) Wait(ctx context.Context) (*ActiveState, error) {
	topic := ClaimTopic(c.OwnMAC)

	resultCh := make(chan *ActiveState, 1)
	errCh := make(chan error, 1)

	if err := c.Transport.Subscribe(topic, func(payload []byte) {
		var resp ClaimResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			errCh <- fmt.Errorf("parse claim: %w", err)
			return
		}
		if err := resp.Verify(c.OwnMAC); err != nil {
			errCh <- fmt.Errorf("verify claim: %w", err)
			return
		}
		state := resp.ToActiveState()
		if err := SaveActiveState(c.StatePath, state); err != nil {
			errCh <- fmt.Errorf("persist state: %w", err)
			return
		}
		slog.Info("Claimed", "controllerId", state.ControllerID, "server", state.ServerHTTPURL)
		resultCh <- state
	}); err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", topic, err)
	}
	slog.Info("Subscribed for claim", "topic", topic)

	select {
	case <-ctx.Done():
		_ = c.Transport.Unsubscribe(topic)
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case state := <-resultCh:
		_ = c.Transport.Unsubscribe(topic)
		return state, nil
	}
}
