package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeTempFile(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestManifestLoadMissingIsEmpty(t *testing.T) {
	m, err := LoadHardwareManifest(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil empty manifest")
	}
	if m.Sensors == nil || len(m.Sensors) != 0 {
		t.Errorf("expected empty sensors, got %+v", m.Sensors)
	}
	if m.Relays == nil || len(m.Relays) != 0 {
		t.Errorf("expected empty relays, got %+v", m.Relays)
	}
}

func TestManifestLoadValid(t *testing.T) {
	body := `
sensors:
  - type: BME280
    protocol: I2C
    i2c_bus: 1
    i2c_addr: 118
    interval: 30
relays:
  - type: LIGHT
    pin: 17
    name: Main Light
  - type: EXHAUST_FAN
    pin: 18
`
	m, err := LoadHardwareManifest(writeTempFile(t, "hw.yaml", body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Sensors) != 1 {
		t.Fatalf("expected 1 sensor, got %d", len(m.Sensors))
	}
	if m.Sensors[0].Type != "BME280" {
		t.Errorf("sensor type: %q", m.Sensors[0].Type)
	}
	if m.Sensors[0].I2CBus == nil || *m.Sensors[0].I2CBus != 1 {
		t.Errorf("sensor i2cBus: %+v", m.Sensors[0].I2CBus)
	}
	if m.Sensors[0].I2CAddr == nil || *m.Sensors[0].I2CAddr != 118 {
		t.Errorf("sensor i2cAddr: %+v", m.Sensors[0].I2CAddr)
	}
	if len(m.Relays) != 2 {
		t.Fatalf("expected 2 relays, got %d", len(m.Relays))
	}
	if m.Relays[1].Pin != 18 {
		t.Errorf("relay pin: %d", m.Relays[1].Pin)
	}
}

func TestManifestRoundTripJSON(t *testing.T) {
	m := &HWManifest{
		Sensors: []Sensor{
			{Type: "BME280", Protocol: "I2C", I2CBus: intPtr(1), I2CAddr: intPtr(118), Interval: intPtr(30)},
		},
		Relays: []Relay{{Type: "LIGHT", Pin: 17, Name: "Main"}},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got HWManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Sensors[0].Type != "BME280" || got.Relays[0].Pin != 17 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func intPtr(n int) *int { return &n }
