package provision

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// BeaconInterval is the spec §2.1 cadence (every 7s).
const BeaconInterval = 7 * time.Second

// BeaconBroadcastAddr is the spec §2.1 UDP broadcast destination.
// Tests override it to a loopback address.
var BeaconBroadcastAddr = net.IPv4bcast.String() + ":9999"

// BeaconSender is the load-bearing abstraction: a connection capable
// of WriteToUDP. Split out so tests can inject a real net.PacketConn.
type BeaconSender interface {
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	Close() error
}

// Beacon is the §2.2 wire payload (JSON).
type Beacon struct {
	Schema       int        `json:"schema"`
	Serial       string     `json:"serial"`
	MAC          string     `json:"mac"`
	IP           string     `json:"ip"`
	FwVersion    string     `json:"fwVersion"`
	ClaimPin     string     `json:"claimPin"`
	PinExpiresAt int64      `json:"pinExpiresAt"`
	HwManifest   HWManifest `json:"hwManifest"`
}

// BuildBeacon constructs the §2.2 JSON payload from the current PIN +
// manifest. Pure — no I/O — so tests can assert the wire form.
func BuildBeacon(now time.Time, pi *PrimaryInterface, serial, fw string, pin PINState, m *HWManifest) []byte {
	if m == nil {
		m = &HWManifest{Sensors: []Sensor{}, Relays: []Relay{}}
	}
	if m.Sensors == nil {
		m.Sensors = []Sensor{}
	}
	if m.Relays == nil {
		m.Relays = []Relay{}
	}
	ip := ""
	mac := ""
	if pi != nil {
		ip = pi.IP
		mac = pi.MAC
	}
	b := Beacon{
		Schema:       1,
		Serial:       serial,
		MAC:          mac,
		IP:           ip,
		FwVersion:    fw,
		ClaimPin:     pin.Pin,
		PinExpiresAt: pin.ExpiresAtMs,
		HwManifest:   *m,
	}
	data, err := json.Marshal(b)
	if err != nil {
		// Beacon struct contains only serialisable fields; marshal cannot
		// fail under any reachable input. Return empty bytes rather than
		// panicking so the broadcaster stays non-fatal.
		slog.Warn("beacon marshal failed", "error", err, "now", now)
		return nil
	}
	return data
}

// DialBeaconSender opens an unconnected UDP socket bound to the
// wildcard address (port chosen by the kernel) so WriteToUDP can
// target any destination — including the §2.1 broadcast. A connected
// DialUDP cannot be WriteTo'd, so we explicitly listen instead.
func DialBeaconSender() (BeaconSender, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("listen beacon udp: %w", err)
	}
	return conn, nil
}

// SendBeaconNow writes a single beacon packet through sender.
// sender is injected so tests can drive a real net.PacketConn on a
// chosen loopback port.
func SendBeaconNow(sender BeaconSender, payload []byte) error {
	dst, err := net.ResolveUDPAddr("udp", BeaconBroadcastAddr)
	if err != nil {
		return fmt.Errorf("resolve beacon dst: %w", err)
	}
	if _, err := sender.WriteToUDP(payload, dst); err != nil {
		return fmt.Errorf("write beacon: %w", err)
	}
	return nil
}
