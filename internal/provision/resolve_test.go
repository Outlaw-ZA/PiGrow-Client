package provision

import (
	"testing"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
)

// intPtr is defined in manifest_test.go (same package).

func TestResolveActive_NoStateYAMLUnchanged(t *testing.T) {
	cfg := &config.Config{
		MQTT:   config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Server: config.ServerConfig{HTTPURL: "http://legacy/api", ControllerID: "legacy-id"},
	}
	got := ResolveActiveConfig(cfg, nil)
	if got.Server.HTTPURL != "http://legacy/api" || got.Server.ControllerID != "legacy-id" {
		t.Errorf("legacy path changed: %+v", got.Server)
	}
}

func TestResolveActive_StateOverridesLegacy(t *testing.T) {
	cfg := &config.Config{
		MQTT:   config.MQTTConfig{Broker: "tcp://legacy:1883", ClientID: "c", Username: "old", Password: "old"},
		Server: config.ServerConfig{HTTPURL: "http://legacy/api", ControllerID: "legacy-id"},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "claimed-id",
		ServerHTTPURL:  "http://server:3000",
		MQTTBrokerURL:  "tcp://server:1883",
		MQTTUsername:   "pigrow-claimed-id",
		MQTTPassword:   "fresh",
	}
	got := ResolveActiveConfig(cfg, state)
	if got.Server.HTTPURL != "http://server:3000" {
		t.Errorf("server URL not overridden: %q", got.Server.HTTPURL)
	}
	if got.Server.ControllerID != "claimed-id" {
		t.Errorf("controllerId not overridden: %q", got.Server.ControllerID)
	}
	if got.MQTT.Broker != "tcp://server:1883" {
		t.Errorf("broker not overridden: %q", got.MQTT.Broker)
	}
	if got.MQTT.Username != "pigrow-claimed-id" || got.MQTT.Password != "fresh" {
		t.Errorf("creds not overridden: %+v", got.MQTT)
	}
}

func TestResolveActive_PartialStateKeepsLegacyOnUnsetFields(t *testing.T) {
	cfg := &config.Config{
		MQTT:   config.MQTTConfig{Broker: "tcp://legacy:1883", ClientID: "c", Username: "old", Password: "old"},
		Server: config.ServerConfig{HTTPURL: "http://legacy/api", ControllerID: "legacy-id"},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "claimed-id",
		ServerHTTPURL:  "http://server:3000",
		// MQTTBrokerURL / Username / Password intentionally empty
	}
	got := ResolveActiveConfig(cfg, state)
	if got.MQTT.Broker != "tcp://legacy:1883" {
		t.Errorf("broker unexpectedly changed: %q", got.MQTT.Broker)
	}
	if got.MQTT.Username != "old" || got.MQTT.Password != "old" {
		t.Errorf("creds unexpectedly changed: %+v", got.MQTT)
	}
	if got.Server.ControllerID != "claimed-id" {
		t.Errorf("controllerId should have changed: %q", got.Server.ControllerID)
	}
}

func TestResolveActive_NonActiveStateYAMLUnchanged(t *testing.T) {
	cfg := &config.Config{
		MQTT:   config.MQTTConfig{Broker: "tcp://legacy:1883", ClientID: "c"},
		Server: config.ServerConfig{HTTPURL: "http://legacy/api", ControllerID: "x"},
	}
	state := &ActiveState{ProvisionState: "PENDING", ControllerID: "y"}
	got := ResolveActiveConfig(cfg, state)
	if got.Server.ControllerID != "x" {
		t.Errorf("non-active state should not override: %q", got.Server.ControllerID)
	}
}

// yamlSensorCfg returns a SensorConfig with the legacy hard-coded ID
// pre-fill, simulating a Pi's config.yaml shipped with the Pi.
func yamlSensorCfg(id, bus string, addr uint16) config.SensorConfig {
	return config.SensorConfig{
		ID:         id,
		Type:       "TEMP_HUMIDITY",
		I2CBus:     bus,
		I2CAddress: addr,
		Interval:   "30s",
	}
}

// stateSensor returns a server-issued Sensor fixture for the given
// (type, bus, addr) tuple.
func stateSensor(id, typ string, bus, addr, interval int) Sensor {
	return Sensor{
		ID:       id,
		Type:     typ,
		Protocol: "I2C",
		I2CBus:   intPtr(bus),
		I2CAddr:  intPtr(addr),
		Interval: intPtr(interval),
	}
}

// TestResolveActive_OverlaySensorsByBusAndAddress is the headline
// test for the wire-sensors fix: the YAML sensor's ID must be
// replaced by the server-issued UUID matched on (Type, I2CBus, I2CAddress).
// The YAML's bus/address/interval survive — the YAML owns the wiring.
func TestResolveActive_OverlaySensorsByBusAndAddress(t *testing.T) {
	cfg := &config.Config{
		MQTT:    config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Sensors: []config.SensorConfig{yamlSensorCfg("legacy-uuid", "/dev/i2c-1", 0x44)},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "claimed",
		ServerHTTPURL:  "http://server",
		Sensors:        []Sensor{stateSensor("server-uuid", "TEMP_HUMIDITY", 1, 0x44, 30)},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Sensors) != 1 {
		t.Fatalf("expected 1 sensor, got %d", len(got.Sensors))
	}
	s := got.Sensors[0]
	if s.ID != "server-uuid" {
		t.Errorf("sensor ID not overlaid: got %q want %q", s.ID, "server-uuid")
	}
	if s.I2CBus != "/dev/i2c-1" {
		t.Errorf("YAML bus path must survive overlay: got %q", s.I2CBus)
	}
	if s.I2CAddress != 0x44 {
		t.Errorf("YAML I2C address must survive overlay: got 0x%X", s.I2CAddress)
	}
	if s.Interval != "30s" {
		t.Errorf("YAML interval must survive overlay: got %q", s.Interval)
	}
}

// TestResolveActive_OverlaySensorsByTypeNotPosition verifies the
// matching rule is "type+bus+addr", not "list index". Re-ordering
// the YAML must still match the same state sensor.
func TestResolveActive_OverlaySensorsByTypeNotPosition(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Sensors: []config.SensorConfig{
			yamlSensorCfg("yaml-1", "/dev/i2c-1", 0x44),
			yamlSensorCfg("yaml-2", "/dev/i2c-1", 0x45),
		},
	}
	// State is in a different order: addr 0x45 first, then 0x44.
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Sensors: []Sensor{
			stateSensor("uuid-45", "TEMP_HUMIDITY", 1, 0x45, 30),
			stateSensor("uuid-44", "TEMP_HUMIDITY", 1, 0x44, 30),
		},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Sensors) != 2 {
		t.Fatalf("expected 2 sensors, got %d", len(got.Sensors))
	}
	// YAML sensors preserve their YAML-side ordering.
	if got.Sensors[0].ID != "uuid-44" || got.Sensors[0].I2CAddress != 0x44 {
		t.Errorf("first YAML sensor not overlaid correctly: %+v", got.Sensors[0])
	}
	if got.Sensors[1].ID != "uuid-45" || got.Sensors[1].I2CAddress != 0x45 {
		t.Errorf("second YAML sensor not overlaid correctly: %+v", got.Sensors[1])
	}
}

// TestResolveActive_EmptyStateSensorsKeepsYAML covers the legacy
// server fallback: when the claim payload didn't carry sensors, the
// YAML's sensors continue to drive the active loop unchanged.
func TestResolveActive_EmptyStateSensorsKeepsYAML(t *testing.T) {
	cfg := &config.Config{
		MQTT:    config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Sensors: []config.SensorConfig{yamlSensorCfg("yaml-id", "/dev/i2c-1", 0x44)},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		// Sensors / Devices nil — legacy server.
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Sensors) != 1 {
		t.Fatalf("expected 1 sensor, got %d", len(got.Sensors))
	}
	if got.Sensors[0].ID != "yaml-id" {
		t.Errorf("YAML ID must survive when state has no sensors: got %q", got.Sensors[0].ID)
	}
}

// TestResolveActive_LegacyStateNoSensorsKey covers the
// backward-compat path: a state.json written before the overlay
// landed has no sensors/devices keys. JSON unmarshal leaves the
// slices nil; the overlay must treat nil as "nothing to overlay" and
// leave the YAML untouched.
func TestResolveActive_LegacyStateNoSensorsKey(t *testing.T) {
	cfg := &config.Config{
		MQTT:    config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Sensors: []config.SensorConfig{yamlSensorCfg("yaml-id", "/dev/i2c-1", 0x44)},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		// Sensors, Devices both nil (legacy file).
	}
	got := ResolveActiveConfig(cfg, state)
	if got.Sensors[0].ID != "yaml-id" {
		t.Errorf("legacy state.json must not override YAML sensors: got %q", got.Sensors[0].ID)
	}
}

// TestResolveActive_StateOnlySensorAppended covers the "config.yaml
// lost a sensor after claim" defensive case. The state sensor is
// appended so the active loop drives it.
func TestResolveActive_StateOnlySensorAppended(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		// YAML has zero sensors.
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Sensors:        []Sensor{stateSensor("server-only-uuid", "TEMP_HUMIDITY", 1, 0x44, 60)},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Sensors) != 1 {
		t.Fatalf("expected 1 state-only sensor, got %d", len(got.Sensors))
	}
	s := got.Sensors[0]
	if s.ID != "server-only-uuid" {
		t.Errorf("state-only sensor ID not preserved: %q", s.ID)
	}
	if s.Type != "TEMP_HUMIDITY" || s.I2CBus != "/dev/i2c-1" || s.I2CAddress != 0x44 {
		t.Errorf("state-only sensor shape incorrect: %+v", s)
	}
	if s.Interval != "60s" {
		t.Errorf("state-only sensor interval not converted: %q", s.Interval)
	}
}

// TestResolveActive_OverlayDevicesByTypeAndPin is the device-side
// mirror of TestResolveActive_OverlaySensorsByBusAndAddress.
func TestResolveActive_OverlayDevicesByTypeAndPin(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Devices: []config.DeviceConfig{
			{ID: "yaml-device-id", Type: "LIGHT", Pin: 17, Name: "YAML Light"},
		},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Devices: []Relay{
			{ID: "server-device-uuid", Type: "LIGHT", Pin: 17, Name: "Main Light"},
		},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(got.Devices))
	}
	d := got.Devices[0]
	if d.ID != "server-device-uuid" {
		t.Errorf("device ID not overlaid: got %q", d.ID)
	}
	if d.Pin != 17 || d.Type != "LIGHT" {
		t.Errorf("device type/pin must survive overlay: %+v", d)
	}
}

// TestResolveActive_OverlayDevicesByTypeAndPinNotPosition verifies
// that device matching is by (Type, Pin), not by list index.
func TestResolveActive_OverlayDevicesByTypeAndPinNotPosition(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Devices: []config.DeviceConfig{
			{ID: "yaml-pin-17", Type: "LIGHT", Pin: 17},
			{ID: "yaml-pin-18", Type: "EXHAUST_FAN", Pin: 18},
		},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Devices: []Relay{
			{ID: "uuid-pin-18", Type: "EXHAUST_FAN", Pin: 18},
			{ID: "uuid-pin-17", Type: "LIGHT", Pin: 17},
		},
	}
	got := ResolveActiveConfig(cfg, state)
	if got.Devices[0].ID != "uuid-pin-17" || got.Devices[0].Pin != 17 {
		t.Errorf("first YAML device (pin 17) not matched: %+v", got.Devices[0])
	}
	if got.Devices[1].ID != "uuid-pin-18" || got.Devices[1].Pin != 18 {
		t.Errorf("second YAML device (pin 18) not matched: %+v", got.Devices[1])
	}
}

// TestResolveActive_YAMLOnlySensorKeptWithWarning is the
// state-missing-for-this-yaml-sensor defensive path. The YAML entry
// is preserved (so the YAML's ID still drives the publish), and the
// warn is observable only via slog (test asserts no panic / no
// drop).
func TestResolveActive_YAMLOnlySensorKeptWithWarning(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		Sensors: []config.SensorConfig{
			yamlSensorCfg("yaml-only-id", "/dev/i2c-1", 0x44),
		},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Sensors: []Sensor{
			// Different (bus, addr) — no match for the YAML entry.
			stateSensor("uuid-elsewhere", "TEMP_HUMIDITY", 2, 0x76, 30),
		},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Sensors) != 2 {
		t.Fatalf("expected 2 sensors (YAML kept + state appended), got %d", len(got.Sensors))
	}
	// Order: YAML first, then unmatched state appended.
	if got.Sensors[0].ID != "yaml-only-id" {
		t.Errorf("YAML-only sensor ID changed: %q", got.Sensors[0].ID)
	}
	if got.Sensors[1].ID != "uuid-elsewhere" {
		t.Errorf("state-only sensor not appended: %+v", got.Sensors[1])
	}
}

// TestResolveActive_DuplicateStateSensorsDeduped guards the
// append-loop dedup: if a malformed server payload carries two
// sensors with identical (type, bus, addr), the resolved config
// must contain only one of them. Without the dedup fix the append
// loop would emit the duplicate twice.
//
// The overlay indexes state sensors by canonical key in a map, so a
// duplicate key overwrites — the latest occurrence in the state
// payload wins (matching the server's upsert semantic).
func TestResolveActive_DuplicateStateSensorsDeduped(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
		// YAML has zero sensors so both state entries hit the
		// unmatched-append branch.
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Sensors: []Sensor{
			stateSensor("uuid-first", "TEMP_HUMIDITY", 1, 0x44, 30),
			stateSensor("uuid-second", "TEMP_HUMIDITY", 1, 0x44, 30),
		},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Sensors) != 1 {
		t.Fatalf("duplicate state sensors must be deduped; got %d sensors", len(got.Sensors))
	}
	// Last occurrence wins (map overwrite matches upsert semantics).
	if got.Sensors[0].ID != "uuid-second" {
		t.Errorf("expected latest duplicate entry to win, got %q", got.Sensors[0].ID)
	}
}

// TestResolveActive_DuplicateStateDevicesDeduped is the device
// counterpart of TestResolveActive_DuplicateStateSensorsDeduped.
func TestResolveActive_DuplicateStateDevicesDeduped(t *testing.T) {
	cfg := &config.Config{
		MQTT: config.MQTTConfig{Broker: "tcp://x", ClientID: "c"},
	}
	state := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "c",
		ServerHTTPURL:  "http://s",
		Devices: []Relay{
			{ID: "dev-1", Type: "LIGHT", Pin: 17},
			{ID: "dev-2", Type: "LIGHT", Pin: 17},
		},
	}
	got := ResolveActiveConfig(cfg, state)
	if len(got.Devices) != 1 {
		t.Fatalf("duplicate state devices must be deduped; got %d devices", len(got.Devices))
	}
	if got.Devices[0].ID != "dev-2" {
		t.Errorf("expected latest duplicate device entry to win, got %q", got.Devices[0].ID)
	}
}
