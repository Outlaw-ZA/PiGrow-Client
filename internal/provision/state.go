// Package provision implements the Pi-side of the PiGrow controller
// auto-provisioning protocol: unclaimed-mode UDP/mDNS discovery beacon,
// MQTT claim handshake, and persistence of active pairing state to a
// separate state.json file.
package provision

import (
	"encoding/json"
	"fmt"
	"os"
)

// ActiveState is what the Pi writes to state.json after a successful
// ClaimResponse. It is the single source of truth for active-mode
// controller ID + broker + server URL; legacy config.yaml is fallback
// when this file is absent (see spec §5 backward compat).
//
// Sensors and Devices mirror the server-issued manifest: the IDs are
// the UUIDs the server assigned, and are overlaid onto the legacy
// config.yaml sensor/device list during active-mode resolution. Older
// state.json files (pre-sensor-overlay) unmarshal to nil slices; the
// overlay treats nil as "nothing to overlay" and the YAML wins.
type ActiveState struct {
	ProvisionState string   `json:"provisionState"`
	ControllerID   string   `json:"controllerId"`
	ControllerMAC  string   `json:"controllerMac"`
	MQTTBrokerURL  string   `json:"mqttBrokerUrl"`
	MQTTUsername   string   `json:"mqttUsername"`
	MQTTPassword   string   `json:"mqttPassword"`
	ServerHTTPURL  string   `json:"serverHttpUrl"`
	PairedAt       int64    `json:"pairedAt"`
	Sensors        []Sensor `json:"sensors,omitempty"`
	Devices        []Relay  `json:"devices,omitempty"`
}

// IsClaimed reports whether this state represents a completed claim
// (controllerId + provisionState==ACTIVE).
func (s *ActiveState) IsClaimed() bool {
	return s != nil && s.ProvisionState == "ACTIVE" && s.ControllerID != ""
}

// LoadActiveState reads state.json from path. Returns (nil, nil) when
// the file does not exist — that is the normal unclaimed-boot signal.
func LoadActiveState(path string) (*ActiveState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s ActiveState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &s, nil
}

// SaveActiveState atomically writes state to path via a temp file +
// rename, so a crash mid-write doesn't leave a half-written state.
func SaveActiveState(path string, s *ActiveState) error {
	if s == nil {
		return fmt.Errorf("nil state")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
