package provision

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// countingSender records every SendBeaconNow call so the test can
// verify that beacon broadcasts halt after a successful claim.
type countingSender struct {
	sends  atomic.Int32
	closed atomic.Bool
}

func (c *countingSender) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	c.sends.Add(1)
	return len(b), nil
}

func (c *countingSender) Close() error {
	c.closed.Store(true)
	return nil
}

// TestRunUnclaimedStopsBeaconOnClaim confirms that the UDP beacon
// stops broadcasting once a successful ClaimResponse persists
// state.json — spec §2.3 ("the Pi stops beaconing and switches to
// active mode"). The test injects a counting sender and asserts
// that the send count freezes after RunUnclaimed returns. With the
// beacon-cancel fix reverted, the count keeps advancing.
func TestRunUnclaimedStopsBeaconOnClaim(t *testing.T) {
	origInterval := BeaconInterval
	origPick := pickInterfaceFn
	defer func() {
		BeaconInterval = origInterval
		pickInterfaceFn = origPick
	}()

	BeaconInterval = 30 * time.Millisecond

	pickInterfaceFn = func() (*PrimaryInterface, error) {
		return &PrimaryInterface{MAC: "AA:BB:CC:DD:EE:FF", IP: "127.0.0.1"}, nil
	}

	dir := t.TempDir()
	serialPath := filepath.Join(dir, "serial.txt")
	statePath := filepath.Join(dir, "state.json")
	hwPath := filepath.Join(dir, "hw.yaml")

	sender := &countingSender{}

	transport := newFakeTransport()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runDone := make(chan *ActiveState, 1)
	runErr := make(chan error, 1)
	go func() {
		st, err := RunUnclaimed(ctx, RunOptions{
			FwVersion:    "test",
			SerialPath:   serialPath,
			HardwarePath: hwPath,
			StatePath:    statePath,
			ClaimMQTT:    transport,
			DialSender:   func() (BeaconSender, error) { return sender, nil },
		})
		if err != nil {
			runErr <- err
			return
		}
		runDone <- st
	}()

	// Wait for subscription to register.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if transport.hasSubscription() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Wait briefly so at least one periodic beacon has fired.
	time.Sleep(60 * time.Millisecond)
	sendsAtClaim := sender.sends.Load()
	if sendsAtClaim < 1 {
		t.Fatalf("expected at least one beacon before claim, got %d", sendsAtClaim)
	}

	resp := ClaimResponse{
		Schema:        1,
		ControllerID:  "ctrl-test",
		ControllerMAC: "AA:BB:CC:DD:EE:FF",
		ServerHTTPURL: "http://server:3000",
		PairedAt:      1737000000000,
	}
	body, _ := json.Marshal(resp)
	transport.inject(ClaimTopic("AA:BB:CC:DD:EE:FF"), body)

	select {
	case st := <-runDone:
		if st == nil {
			t.Fatal("RunUnclaimed returned nil state")
		}
	case err := <-runErr:
		t.Fatalf("RunUnclaimed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("RunUnclaimed did not return")
	}

	// After RunUnclaimed returned, deferred beaconCancel + sender.Close
	// have run. The send count must not advance further, and the
	// ticker/sender goroutines spawned inside RunUnclaimed must have
	// exited (spec §2.3 "the Pi stops beaconing").
	const wait = 150 * time.Millisecond
	time.Sleep(wait)
	sendsAfterClaim := sender.sends.Load()
	if sendsAfterClaim > sendsAtClaim {
		t.Errorf("beacon kept broadcasting after claim: at-claim=%d after=%d (delta=%d over %s)",
			sendsAtClaim, sendsAfterClaim, sendsAfterClaim-sendsAtClaim, wait)
	}

	// Goroutine leak check: without the cancel-on-return fix the
	// ticker + sender goroutines spawned inside RunUnclaimed outlive
	// it, so runtime.NumGoroutine() is ≥4 here; with the fix the
	// two goroutines exit shortly after RunUnclaimed returns and
	// the count is 2 (the test's own runnables only).
	time.Sleep(150 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > 3 {
		t.Errorf("goroutine leak suspected after claim: %d goroutines alive", got)
	}
}
