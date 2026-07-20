package provision

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Sensor is one entry in hwManifest.sensors[] per spec §2.2.
type Sensor struct {
	Type     string `json:"type" yaml:"type"`
	Protocol string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	I2CBus   *int   `json:"i2cBus,omitempty" yaml:"i2c_bus,omitempty"`
	I2CAddr  *int   `json:"i2cAddr,omitempty" yaml:"i2c_addr,omitempty"`
	Pin      *int   `json:"pin,omitempty" yaml:"pin,omitempty"`
	Interval *int   `json:"interval,omitempty" yaml:"interval,omitempty"`
}

// Relay is one entry in hwManifest.relays[] per spec §2.2.
type Relay struct {
	Type string `json:"type" yaml:"type"`
	Pin  int    `json:"pin" yaml:"pin"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// HWManifest is the Pi's wired-hardware description carried in the
// ProvisionBeacon. An empty (zero-value) manifest is valid: spec §6
// explicitly puts GPIO/I2C auto-detect out of scope; Pis without a
// hardware.yaml broadcast sensors:[], relays:[] and can still be claimed.
type HWManifest struct {
	Sensors []Sensor `json:"sensors" yaml:"sensors"`
	Relays  []Relay  `json:"relays" yaml:"relays"`
}

// LoadHardwareManifest reads a hardware.yaml from path. If the file is
// missing, returns an empty manifest and no error so a Pi without
// wired hardware can still beacon.
func LoadHardwareManifest(path string) (*HWManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &HWManifest{Sensors: []Sensor{}, Relays: []Relay{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m HWManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Sensors == nil {
		m.Sensors = []Sensor{}
	}
	if m.Relays == nil {
		m.Relays = []Relay{}
	}
	return &m, nil
}
