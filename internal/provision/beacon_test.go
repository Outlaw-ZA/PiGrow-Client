package provision

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

// packetConnSender wraps a net.PacketConn so it satisfies BeaconSender.
// Only WriteToUDP is exercised by the broadcaster; Close frees the
// underlying socket so tests don't leak fds.
type packetConnSender struct {
	conn net.PacketConn
	mu   sync.Mutex
}

func (p *packetConnSender) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.WriteTo(b, addr)
}
func (p *packetConnSender) Close() error { return p.conn.Close() }

// dialTestSender opens an unconnected UDP socket on an ephemeral port,
// ready for WriteToUDP against any destination.
func dialTestSender(t *testing.T) *packetConnSender {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("listen test sender: %v", err)
	}
	return &packetConnSender{conn: conn}
}

func TestBeaconJSONShape(t *testing.T) {
	pi := &PrimaryInterface{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.42"}
	pin := PINState{Pin: "123456", ExpiresAtMs: 1737000000000}
	manifest := &HWManifest{
		Sensors: []Sensor{
			{Type: "BME280", Protocol: "I2C", I2CBus: intPtr(1), I2CAddr: intPtr(118), Interval: intPtr(30)},
		},
		Relays: []Relay{{Type: "LIGHT", Pin: 17, Name: "Main Light"}},
	}
	payload := BuildBeacon(time.UnixMilli(1737000000000), pi, "PIGROW-A1B2C3", "0.4.0", pin, manifest)
	if len(payload) == 0 {
		t.Fatal("empty beacon payload")
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["schema"].(float64) != 1 {
		t.Errorf("schema: %v", got["schema"])
	}
	if got["serial"].(string) != "PIGROW-A1B2C3" {
		t.Errorf("serial: %v", got["serial"])
	}
	if got["mac"].(string) != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("mac: %v", got["mac"])
	}
	if got["ip"].(string) != "192.168.1.42" {
		t.Errorf("ip: %v", got["ip"])
	}
	if got["fwVersion"].(string) != "0.4.0" {
		t.Errorf("fwVersion: %v", got["fwVersion"])
	}
	if got["claimPin"].(string) != "123456" {
		t.Errorf("claimPin: %v", got["claimPin"])
	}
	if got["pinExpiresAt"].(float64) != 1737000000000 {
		t.Errorf("pinExpiresAt: %v", got["pinExpiresAt"])
	}

	hw, ok := got["hwManifest"].(map[string]any)
	if !ok {
		t.Fatalf("hwManifest missing or wrong type: %T", got["hwManifest"])
	}
	sensors, _ := hw["sensors"].([]any)
	if len(sensors) != 1 {
		t.Fatalf("sensors: %v", hw["sensors"])
	}
	s0, _ := sensors[0].(map[string]any)
	if s0["type"].(string) != "BME280" {
		t.Errorf("sensor type: %v", s0)
	}
}

func TestBeaconEmptyManifestIsValid(t *testing.T) {
	payload := BuildBeacon(time.Now(), nil, "S", "0.4.0", PINState{Pin: "000000"}, nil)
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	hw := got["hwManifest"].(map[string]any)
	if hws, _ := hw["sensors"].([]any); len(hws) != 0 {
		t.Errorf("expected empty sensors, got %v", hws)
	}
	if hwr, _ := hw["relays"].([]any); len(hwr) != 0 {
		t.Errorf("expected empty relays, got %v", hwr)
	}
}

func TestSendBeaconRoundtripOnLoopback(t *testing.T) {
	// Receiver: bind a UDP socket on an ephemeral loopback port.
	recv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer recv.Close()

	// Override the spec broadcast dest for this test.
	orig := BeaconBroadcastAddr
	defer func() { BeaconBroadcastAddr = orig }()
	BeaconBroadcastAddr = recv.LocalAddr().String()

	sender := dialTestSender(t)
	defer sender.Close()

	pi := &PrimaryInterface{MAC: "AA:BB:CC:DD:EE:FF", IP: "127.0.0.1"}
	payload := BuildBeacon(time.Now(), pi, "PIGROW-DEAD12", "0.4.0",
		PINState{Pin: "045678", ExpiresAtMs: time.Now().Add(5 * time.Minute).UnixMilli()},
		nil)

	if err := SendBeaconNow(sender, payload); err != nil {
		t.Fatalf("send: %v", err)
	}

	_ = recv.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := recv.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	got := buf[:n]

	var b Beacon
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatalf("parse beacon: %v", err)
	}
	if b.Serial != "PIGROW-DEAD12" {
		t.Errorf("serial %q != DEAD12", b.Serial)
	}
	if b.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("mac %q != AA:BB:CC:DD:EE:FF", b.MAC)
	}
	if b.ClaimPin != "045678" {
		t.Errorf("pin %q != 045678", b.ClaimPin)
	}
	if b.Schema != 1 {
		t.Errorf("schema %d != 1", b.Schema)
	}
}

func TestBeaconPinRangeAndZeroPad(t *testing.T) {
	// Verify BuildBeacon marshals a tiny PIN with leading zero intact
	// (JSON strings preserve ordering).
	payload := BuildBeacon(time.Now(), nil, "S", "v", PINState{Pin: "000042"}, nil)
	if !jsonContains(payload, `"claimPin":"000042"`) {
		t.Fatalf("pin not zero-padded in payload: %s", payload)
	}
}

func jsonContains(haystack []byte, needle string) bool {
	return len(haystack) >= len(needle) && contains(haystack, needle)
}

func contains(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
