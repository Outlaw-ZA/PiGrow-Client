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

	// DialSender overrides the UDP-broadcast socket factory. nil =
	// DialBeaconSender. Tests use this to inject a counting spy
	// without exposing real-network side effects.
	DialSender func() (BeaconSender, error)
}

// pickInterfaceFn is a package-level seam so tests can stub network
// interface discovery without pulling in a real interface or
// re-architecting RunUnclaimed's signature.
var pickInterfaceFn = PickPrimaryInterface

// RunUnclaimed holds the Pi in unclaimed mode: it starts the UDP
// beacon (and mDNS if available) and blocks waiting for a valid
// ClaimResponse. On success it returns the persisted ActiveState —
// main then reads state.json and continues into the active loop.
//
// Beacon goroutines are bounded to a child context that is cancelled
// on return, so a successful claim stops the beacon per spec §2.3
// "the Pi stops beaconing and switches to active mode" without
// depending on the caller's ctx.
//
// When the caller's ctx is cancelled before a claim arrives,
// RunUnclaimed returns ctx.Err(); the caller is responsible for
// deciding whether to fall back to active mode or exit.
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

	pi, err := pickInterfaceFn()
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

	dialSender := opts.DialSender
	if dialSender == nil {
		dialSender = DialBeaconSender
	}
	sender, err := dialSender()
	if err != nil {
		return nil, fmt.Errorf("dial beacon sender: %w", err)
	}
	defer sender.Close()

	// beaconCtx bounds beacon + mDNS goroutines to RunUnclaimed's
	// lifetime so they stop on a successful claim (spec §2.3) without
	// forcing the caller to cancel its parent ctx.
	beaconCtx, beaconCancel := context.WithCancel(ctx)
	defer beaconCancel()

	beaconCh := make(chan []byte, 8)

	// First beacon sent immediately on start so the server doesn't
	// have to wait a full BeaconInterval before the first sighting.
	select {
	case beaconCh <- BuildBeacon(time.Now(), pi, pin.Serial(), opts.FwVersion, pin.Current(), manifest):
	default:
	}

	go func() {
		defer close(beaconCh)
		ticker := time.NewTicker(BeaconInterval)
		defer ticker.Stop()
		for {
			select {
			case <-beaconCtx.Done():
				return
			case <-ticker.C:
				payload := BuildBeacon(time.Now(), pi, pin.Serial(), opts.FwVersion, pin.Current(), manifest)
				select {
				case beaconCh <- payload:
				default:
					// Consumer slower than beacon; drop oldest — only
					// the freshest payload matters.
				}
			}
		}
	}()

	// Sender goroutine owns no socket lifecycle; the outer defer
	// sender.Close handles teardown.
	go func() {
		for payload := range beaconCh {
			if err := SendBeaconNow(sender, payload); err != nil {
				slog.Warn("Beacon send failed", "error", err)
			}
		}
	}()

	// mDNS advertiser — best-effort, log-warn on failure but don't
	// block claiming on it (UDP beacon is primary per spec §2.1).
	adv := NewMDNSAdvertiser(pin.Serial(), pi.MAC, pi.IP, opts.FwVersion, pin)
	if shutdown, err := adv.Start(beaconCtx); err != nil {
		slog.Warn("mDNS advertiser failed to start", "error", err)
	} else {
		defer shutdown()
	}

	sub := NewClaimSubscriber(opts.ClaimMQTT, pi.MAC, opts.StatePath)
	state, err := sub.Wait(ctx)
	if err != nil {
		return nil, err
	}

	// Spec §2.3 single-use: invalidate the live PIN so a stray
	// post-claim PIN — already stopped from broadcasting, but still
	// held in memory — can't be reused.
	pin.Invalidate()
	slog.Info("Beacon stopped", "controllerId", state.ControllerID)
	return state, nil
}
