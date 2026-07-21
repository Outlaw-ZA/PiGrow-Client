package provision

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
)

// ResolveActiveConfig returns the active-mode configuration: state.json
// fields (controllerId, server URL, broker URL+credentials, sensor IDs,
// device IDs) override legacy config.yaml when a claim has succeeded;
// otherwise the legacy config.yaml is returned untouched.
//
// Sensor matching is by (Type, I2CBus number, I2CAddress) for I2C
// sensors and by (Type, Pin) for ONEWIRE/GPIO sensors — the YAML
// knows the hardware wiring (bus path, address, interval) and the
// state.json knows the server-assigned UUID. The overlay replaces the
// YAML sensor's ID with the server's UUID; bus, address, and
// interval stay from the YAML.
//
// Device matching is by (Type, Pin). The YAML DeviceConfig's ID is
// replaced with the server-assigned UUID from state.json.
//
// Mismatches (defensive — shouldn't happen post-claim):
//   - YAML sensor/device with no matching state.json entry: kept
//     untouched, logged at WARN. Means state.json is stale (no
//     overlay UUID available) — the YAML's ID wins.
//   - state.json sensor/device with no matching YAML entry: appended
//     to the resolved config so the active loop drives it. For
//     sensors this requires an I2C sensor shape; Pin-based state
//     sensors we can't represent in the YAML SensorConfig are logged
//     and skipped (a future Pin-aware SensorConfig can pick them up).
func ResolveActiveConfig(yamlCfg *config.Config, st *ActiveState) *config.Config {
	if yamlCfg == nil {
		return nil
	}
	if st == nil || !st.IsClaimed() {
		return yamlCfg
	}
	out := *yamlCfg
	out.Server.HTTPURL = st.ServerHTTPURL
	out.Server.ControllerID = st.ControllerID
	if st.MQTTBrokerURL != "" {
		out.MQTT.Broker = st.MQTTBrokerURL
	}
	if st.MQTTUsername != "" {
		out.MQTT.Username = st.MQTTUsername
	}
	if st.MQTTPassword != "" {
		out.MQTT.Password = st.MQTTPassword
	}
	out.Sensors = overlaySensors(out.Sensors, st.Sensors)
	out.Devices = overlayDevices(out.Devices, st.Devices)
	return &out
}

// overlaySensors merges server-issued sensors (from state.json) onto
// the YAML's sensor list. See ResolveActiveConfig for the matching
// algorithm and the mismatch-handling policy.
func overlaySensors(yamlSensors []config.SensorConfig, stateSensors []Sensor) []config.SensorConfig {
	if len(stateSensors) == 0 {
		return yamlSensors
	}

	stateIndex := make(map[string]Sensor)
	stateOrder := make([]string, 0, len(stateSensors))
	for _, ss := range stateSensors {
		if ss.ID == "" {
			// No server-issued ID — nothing to overlay with. Skip
			// rather than emit a synthetic YAML entry that would
			// publish to sensors//telemetry.
			continue
		}
		key := sensorStateKey(ss)
		if key == "" {
			slog.Warn("State sensor has no addressable key, skipping",
				"type", ss.Type, "id", ss.ID)
			continue
		}
		stateIndex[key] = ss
		stateOrder = append(stateOrder, key)
	}

	out := make([]config.SensorConfig, 0, len(yamlSensors)+len(stateIndex))
	matched := make(map[string]bool, len(stateIndex))
	for _, ys := range yamlSensors {
		key := yamlSensorKey(ys)
		if key == "" {
			// YAML sensor has an unparseable bus path — leave it
			// untouched but log so misconfiguration is visible.
			slog.Warn("YAML sensor has unparseable I2C bus path, skipping overlay",
				"type", ys.Type, "id", ys.ID, "i2c_bus", ys.I2CBus)
			out = append(out, ys)
			continue
		}
		if ss, ok := stateIndex[key]; ok {
			ys.ID = ss.ID
			matched[key] = true
		} else {
			slog.Warn("No state.json sensor matches YAML sensor, keeping YAML ID",
				"type", ys.Type, "yamlId", ys.ID, "key", key)
		}
		out = append(out, ys)
	}

	// Append state sensors that have no YAML counterpart. This
	// covers the case where someone removed sensors from
	// config.yaml after the claim.
	for _, key := range stateOrder {
		if matched[key] {
			continue
		}
		ss := stateIndex[key]
		cfg, ok := convertStateSensorToConfig(ss)
		if !ok {
			slog.Warn("State sensor cannot be represented in YAML config (no I2C address), skipping",
				"type", ss.Type, "id", ss.ID)
			continue
		}
		slog.Info("Appending state-only sensor", "type", ss.Type, "id", ss.ID)
		out = append(out, cfg)
	}
	return out
}

// overlayDevices merges server-issued devices onto the YAML's device
// list. See ResolveActiveConfig for the matching algorithm.
func overlayDevices(yamlDevices []config.DeviceConfig, stateDevices []Relay) []config.DeviceConfig {
	if len(stateDevices) == 0 {
		return yamlDevices
	}

	stateIndex := make(map[string]Relay)
	stateOrder := make([]string, 0, len(stateDevices))
	for _, sd := range stateDevices {
		if sd.ID == "" {
			continue
		}
		key := deviceKey(sd.Type, sd.Pin)
		stateIndex[key] = sd
		stateOrder = append(stateOrder, key)
	}

	out := make([]config.DeviceConfig, 0, len(yamlDevices)+len(stateIndex))
	matched := make(map[string]bool, len(stateIndex))
	for _, yd := range yamlDevices {
		key := deviceKey(yd.Type, yd.Pin)
		if sd, ok := stateIndex[key]; ok {
			yd.ID = sd.ID
			matched[key] = true
		} else {
			slog.Warn("No state.json device matches YAML device, keeping YAML ID",
				"type", yd.Type, "yamlId", yd.ID, "pin", yd.Pin)
		}
		out = append(out, yd)
	}

	for _, key := range stateOrder {
		if matched[key] {
			continue
		}
		sd := stateIndex[key]
		out = append(out, convertStateDeviceToConfig(sd))
		slog.Info("Appending state-only device", "type", sd.Type, "id", sd.ID, "pin", sd.Pin)
	}
	return out
}

// sensorStateKey is the canonical matching key for a server-issued
// sensor. I2C sensors key on (type, bus, addr); non-I2C sensors key
// on (type, pin). Returns "" when the sensor has neither an I2C
// address nor a pin — the overlay can't use it.
func sensorStateKey(s Sensor) string {
	if s.I2CBus != nil && s.I2CAddr != nil {
		return fmt.Sprintf("i2c|%s|%d|%d", s.Type, *s.I2CBus, *s.I2CAddr)
	}
	if s.Pin != nil {
		return fmt.Sprintf("pin|%s|%d", s.Type, *s.Pin)
	}
	return ""
}

// yamlSensorKey is the canonical matching key for a YAML sensor.
// YAML sensors are I2C-only (no Pin field), so the bus path is parsed
// into a number and combined with (type, address). Returns "" on an
// unparseable bus path.
func yamlSensorKey(ys config.SensorConfig) string {
	bus, err := strconv.Atoi(strings.TrimPrefix(ys.I2CBus, "/dev/i2c-"))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("i2c|%s|%d|%d", ys.Type, bus, ys.I2CAddress)
}

// deviceKey is the canonical matching key for both YAML and
// state-issued devices — they share the (type, pin) shape.
func deviceKey(typ string, pin int) string {
	return fmt.Sprintf("%s|%d", typ, pin)
}

// convertStateSensorToConfig synthesises a YAML SensorConfig from a
// state-issued Sensor. The server interval is in seconds (int);
// convert to the YAML's duration-string form so ParseInterval works.
// Returns ok=false for non-I2C sensors — they can't be expressed in
// the current YAML schema.
func convertStateSensorToConfig(s Sensor) (config.SensorConfig, bool) {
	if s.I2CBus == nil || s.I2CAddr == nil {
		return config.SensorConfig{}, false
	}
	intervalSec := 30 // sane default; server's spec-default interval.
	if s.Interval != nil && *s.Interval > 0 {
		intervalSec = *s.Interval
	}
	return config.SensorConfig{
		ID:         s.ID,
		Type:       s.Type,
		I2CBus:     fmt.Sprintf("/dev/i2c-%d", *s.I2CBus),
		I2CAddress: uint16(*s.I2CAddr),
		Interval:   fmt.Sprintf("%ds", intervalSec),
	}, true
}

// convertStateDeviceToConfig synthesises a YAML DeviceConfig from a
// state-issued Relay. Always succeeds — devices carry every field
// the YAML needs.
func convertStateDeviceToConfig(d Relay) config.DeviceConfig {
	return config.DeviceConfig{
		ID:   d.ID,
		Type: d.Type,
		Pin:  d.Pin,
		Name: d.Name,
	}
}
