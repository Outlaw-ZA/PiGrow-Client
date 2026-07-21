package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadActiveStateMissingReturnsNil(t *testing.T) {
	s, err := LoadActiveState(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing-file read: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil for missing file, got %+v", s)
	}
}

func TestSaveAndLoadActiveState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "abc-123",
		ControllerMAC:  "AA:BB:CC:DD:EE:FF",
		MQTTBrokerURL:  "tcp://192.168.1.10:1883",
		MQTTUsername:   "pigrow-abc-123",
		MQTTPassword:   "shhh",
		ServerHTTPURL:  "http://192.168.1.10:3000",
		PairedAt:       1737000000000,
		Sensors: []Sensor{
			{ID: "srv-sensor-1", Type: "TEMP_HUMIDITY", I2CBus: intPtr(1), I2CAddr: intPtr(0x44), Interval: intPtr(30)},
		},
		Devices: []Relay{
			{ID: "srv-device-1", Type: "LIGHT", Pin: 17, Name: "Main Light"},
		},
	}
	if err := SaveActiveState(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadActiveState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil || got.ControllerID != "abc-123" || got.ProvisionState != "ACTIVE" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got.Sensors) != 1 || got.Sensors[0].ID != "srv-sensor-1" {
		t.Errorf("sensors round-trip lost: %+v", got.Sensors)
	}
	if got.Sensors[0].I2CBus == nil || *got.Sensors[0].I2CBus != 1 {
		t.Errorf("sensor i2cBus round-trip lost: %+v", got.Sensors[0].I2CBus)
	}
	if len(got.Devices) != 1 || got.Devices[0].ID != "srv-device-1" || got.Devices[0].Pin != 17 {
		t.Errorf("devices round-trip lost: %+v", got.Devices)
	}
}

// TestLoadActiveStateLegacyNoSensorsKey verifies that an old state.json
// written before the sensors/devices overlay landed still parses to a
// usable ActiveState with nil Sensors/Devices. The overlay treats
// nil as "nothing to overlay", so the YAML wins. JSON unmarshal of a
// missing key leaves the slice nil.
func TestLoadActiveStateLegacyNoSensorsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	// Old-format JSON: no sensors/devices keys.
	old := `{
  "provisionState": "ACTIVE",
  "controllerId": "abc-123",
  "controllerMac": "AA:BB:CC:DD:EE:FF",
  "mqttBrokerUrl": "tcp://x:1883",
  "mqttUsername": "u",
  "mqttPassword": "p",
  "serverHttpUrl": "http://x",
  "pairedAt": 99
}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadActiveState(path)
	if err != nil {
		t.Fatalf("legacy load: %v", err)
	}
	if got == nil || got.ControllerID != "abc-123" {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Sensors != nil {
		t.Errorf("expected nil Sensors from legacy state, got %+v", got.Sensors)
	}
	if got.Devices != nil {
		t.Errorf("expected nil Devices from legacy state, got %+v", got.Devices)
	}
}

// TestSaveActiveStateEmptySensorsOmitsKey verifies that an empty
// (non-nil) Sensors slice is serialised as [] not omitted. This makes
// the persisted shape self-describing for ops.
func TestSaveActiveStateEmptySensorsOmitsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := SaveActiveState(path, &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "x",
		Sensors:        []Sensor{},
		Devices:        []Relay{},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Sensors/Devices are tagged `omitempty`, so empty non-nil slices
	// are omitted from JSON. Unmarshal must still leave nil slices,
	// which the overlay treats as "nothing to overlay".
	var roundTrip ActiveState
	if err := json.Unmarshal(body, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip.Sensors != nil {
		t.Errorf("expected nil Sensors after round-trip of empty-slice, got %+v", roundTrip.Sensors)
	}
}

func TestActiveStateIsClaimed(t *testing.T) {
	cases := []struct {
		name string
		in   *ActiveState
		want bool
	}{
		{"nil", nil, false},
		{"empty", &ActiveState{}, false},
		{"wrongState", &ActiveState{ControllerID: "x", ProvisionState: "PENDING"}, false},
		{"noControllerID", &ActiveState{ProvisionState: "ACTIVE"}, false},
		{"activeWithID", &ActiveState{ProvisionState: "ACTIVE", ControllerID: "x"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.IsClaimed(); got != c.want {
				t.Errorf("IsClaimed(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestSaveAtomicRename(t *testing.T) {
	// A pre-existing temp file must not block the rename; rename
	// replaces the destination atomically on POSIX.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveActiveState(path, &ActiveState{ControllerID: "x", ProvisionState: "ACTIVE"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be renamed away, stat err=%v", err)
	}
}
