package provision

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RunOptions tunes the unclaimed-boot provisioning loop.
type RunOptions struct {
	// FwVersion is the PiGrow-Client version baked into the beacon.
	FwVersion string

	// SerialPath is where the persistent device serial lives
	// (default "./serial.txt" in main).
	SerialPath string

	// HardwarePath is the optional hardware.yaml path; empty = empty manifest.
	HardwarePath string

	// StatePath is where the active state.json will be written after a claim.
	StatePath string

	// BeaconAddr overrides the default 255.255.255.255:9999 broadcast target.
	// Tests use this to drive a local PacketConn on a chosen port.
	BeaconAddr string

	// ClaimMQTT is the transport used to subscribe to the claim topic.
	// Build in main from the existing internal/mqtt client; tests can
	// pass a fake.
	ClaimMQTT ClaimTransport
}

// RunUnclaimed holds the Pi in unclaimed mode: it starts the UDP
// beacon (and mDNS if available) and blocks waiting for a valid
// ClaimResponse. On success it returns the persisted ActiveState —
// main then reads state.json and continues into the active loop.
//
// When the caller's ctx is cancelled before a claim arrives, RunUnclaimed
// returns ctx.Err(); the caller is responsible for deciding whether to
// fall back to active mode or exit.
func RunUnclaimed(ctx context.Context, opts RunOptions) (*ActiveState, error) {
	if opts.SerialPath == "" {
		opts.SerialPath = "./serial.txt"
	}
	if opts.StatePath == "" {
		opts.StatePath = "./state.json"
	}
	if opts.FwVersion == "" {
		opts.FwVersion = "0.0.0-unset"
	}

	pin, err := NewPin(opts.SerialPath)
	if err != nil {
		return nil, fmt.Errorf("init pin: %w", err)
	}
	slog.Info("Device serial", "serial", pin.Serial())

	pi, err := PickPrimaryInterface()
	if err != nil {
		// Not fatal: the operator can wire a stub MAC for headless tests.
		// Beacon still goes out, server can't key on MAC — fail loud.
		return nil, fmt.Errorf("primary interface: %w", err)
	}
	slog.Info("Primary interface", "mac", pi.MAC, "ip", pi.IP)

	manifest, err := LoadHardwareManifest(opts.HardwarePath)
	if err != nil {
		return nil, fmt.Errorf("hardware manifest: %w", err)
	}
	slog.Info("Hardware manifest loaded", "path", opts.HardwarePath,
		"sensors", len(manifest.Sensors), "relays", len(manifest.Relays))

	if opts.BeaconAddr != "" {
		BeaconBroadcastAddr = opts.BeaconAddr
	}

	sender, err := DialBeaconSender()
	if err != nil {
		return nil, fmt.Errorf("dial beacon sender: %w", err)
	}
	defer sender.Close()

	// First-beacon payload snapshotted before launching the goroutine;
	// subsequent payloads reflect the same PIN until it expires — the
	// goroutine re-snapshots each tick so rotation propagates.
	beaconCh := make(chan []byte, 8)
	go func() {
		defer close(beaconCh)
		ticker := time.NewTicker(BeaconInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state := pin.Current()
				payload := BuildBeacon(time.Now(), pi, pin.Serial(), opts.FwVersion, state, manifest)
				select {
				case beaconCh <- payload:
				default:
					// Consumer running slower than beacon; drop oldest
					// signal — only the freshest payload matters.
				}
			}
		}
	}()

	// Drive the send loop from the channel until ctx is done.
	go func() {
		defer sender.Close()
		for payload := range beaconCh {
			if err := SendBeaconNow(sender, payload); err != nil {
				slog.Warn("Beacon send failed", "error", err)
			}
		}
	}()

	// mDNS advertiser — best-effort, log-warn on failure but don't
	// block claiming on it (UDP beacon is primary per spec §2.1).
	adv := NewMDNSAdvertiser(pin.Serial(), pi.MAC, pi.IP, opts.FwVersion, pin)
	if shutdown, err := adv.Start(ctx); err != nil {
		slog.Warn("mDNS advertiser failed to start", "error", err)
	} else {
		defer shutdown()
	}

	sub := NewClaimSubscriber(opts.ClaimMQTT, pi.MAC, opts.StatePath)
	return sub.Wait(ctx)
}
