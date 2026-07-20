package provision

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClaimTransport captures subscriptions and lets a test inject a
// payload through the registered handler.
type fakeClaimTransport struct {
	mu  sync.Mutex
	subs map[string]ClaimHandler
}

func newFakeTransport() *fakeClaimTransport { return &fakeClaimTransport{subs: map[string]ClaimHandler{}} }

func (f *fakeClaimTransport) Subscribe(topic string, h ClaimHandler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[topic] = h
	return nil
}
func (f *fakeClaimTransport) Unsubscribe(topic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, topic)
	return nil
}
func (f *fakeClaimTransport) Connected() bool { return true }
func (f *fakeClaimTransport) inject(topic string, payload []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h, ok := f.subs[topic]; ok {
		h(payload)
	}
}

// hasSubscription reports whether Subscribe has been called for any
// topic yet — used by tests that race a Subscribe against their own
// observer. Locks the underlying map; safe under -race.
func (f *fakeClaimTransport) hasSubscription() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs) > 0
}

var _ ClaimTransport = (*fakeClaimTransport)(nil)

func TestClaimTopicFormat(t *testing.T) {
	got := ClaimTopic("aa:bb:cc:dd:ee:ff")
	want := "provision/AA:BB:CC:DD:EE:FF/claim"
	if got != want {
		t.Errorf("topic: got %q want %q", got, want)
	}
}

func TestVerifySchemaAndMac(t *testing.T) {
	own := "AA:BB:CC:DD:EE:FF"
	tests := []struct {
		name    string
		mutate  func(*ClaimResponse)
		wantErr bool
	}{
		{"valid", func(*ClaimResponse) {}, false},
		{"schema0", func(r *ClaimResponse) { r.Schema = 0 }, true},
		{"schema2", func(r *ClaimResponse) { r.Schema = 2 }, true},
		{"macLower", func(r *ClaimResponse) { r.ControllerMAC = "aa:bb:cc:dd:ee:ff" }, false},
		{"macMismatch", func(r *ClaimResponse) { r.ControllerMAC = "11:22:33:44:55:66" }, true},
		{"missingControllerID", func(r *ClaimResponse) { r.ControllerID = "" }, true},
		{"missingServerURL", func(r *ClaimResponse) { r.ServerHTTPURL = "" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClaimResponse{
				Schema:        1,
				ControllerID:  "ctrl-1",
				ControllerMAC: own,
				ServerHTTPURL: "http://x",
				PairedAt:      1,
			}
			tc.mutate(r)
			err := r.Verify(own)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			} else if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestClaimSubscriberWaitSuccess(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	own := "AA:BB:CC:DD:EE:FF"
	transport := newFakeTransport()
	sub := NewClaimSubscriber(transport, own, statePath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		// Wait for subscription to register, then deliver the claim.
		time.Sleep(30 * time.Millisecond)
		resp := ClaimResponse{
			Schema:        1,
			ControllerID:  "ctrl-abc",
			ControllerMAC: own,
			MQTTBrokerURL: "tcp://192.168.1.10:1883",
			MQTTUsername:  "pigrow-ctrl-abc",
			MQTTPassword:  "shhh",
			ServerHTTPURL: "http://192.168.1.10:3000",
			PairedAt:      1737000000000,
		}
		body, _ := json.Marshal(resp)
		transport.inject(ClaimTopic(own), body)
	}()

	state, err := sub.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if state == nil || state.ControllerID != "ctrl-abc" || state.ProvisionState != "ACTIVE" {
		t.Errorf("unexpected state: %+v", state)
	}
	if state.ServerHTTPURL != "http://192.168.1.10:3000" {
		t.Errorf("serverHttpUrl: %q", state.ServerHTTPURL)
	}
}

func TestClaimSubscriberMACMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	own := "AA:BB:CC:DD:EE:FF"
	transport := newFakeTransport()
	sub := NewClaimSubscriber(transport, own, statePath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(30 * time.Millisecond)
		resp := ClaimResponse{
			Schema:        1,
			ControllerID:  "ctrl-abc",
			ControllerMAC: "11:22:33:44:55:66", // mismatch
			ServerHTTPURL: "http://x",
			PairedAt:      1,
		}
		body, _ := json.Marshal(resp)
		transport.inject(ClaimTopic(own), body)
	}()

	_, err := sub.Wait(ctx)
	if err == nil {
		t.Fatal("expected mac-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected mismatch error, got: %v", err)
	}
}

func TestClaimSubscriberSchemaMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	own := "AA:BB:CC:DD:EE:FF"
	transport := newFakeTransport()
	sub := NewClaimSubscriber(transport, own, statePath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(30 * time.Millisecond)
		resp := ClaimResponse{Schema: 99, ControllerMAC: own, ControllerID: "x", ServerHTTPURL: "http://x", PairedAt: 1}
		body, _ := json.Marshal(resp)
		transport.inject(ClaimTopic(own), body)
	}()

	_, err := sub.Wait(ctx)
	if err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("expected schema error, got: %v", err)
	}
}

func TestClaimSubscriberContextCancel(t *testing.T) {
	dir := t.TempDir()
	transport := newFakeTransport()
	sub := NewClaimSubscriber(transport, "AA:BB:CC:DD:EE:FF", filepath.Join(dir, "state.json"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sub.Wait(ctx); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestToActiveStateShape(t *testing.T) {
	r := &ClaimResponse{
		Schema:        1,
		ControllerID:  "x",
		ControllerMAC: "aa:bb:cc:dd:ee:ff",
		MQTTBrokerURL: "tcp://x",
		MQTTUsername:  "u",
		MQTTPassword:  "p",
		ServerHTTPURL: "http://x",
		PairedAt:      99,
	}
	s := r.ToActiveState()
	if s.ProvisionState != "ACTIVE" || s.ControllerID != "x" || s.ControllerMAC != "AA:BB:CC:DD:EE:FF" || s.PairedAt != 99 {
		t.Errorf("unexpected active state: %+v", s)
	}
}
