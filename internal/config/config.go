package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the full application configuration.
type Config struct {
	MQTT    MQTTConfig     `yaml:"mqtt"`
	Server  ServerConfig   `yaml:"server"`
	Sensors []SensorConfig `yaml:"sensors"`
}

// ServerConfig holds the PiGrow-Server REST API connection settings.
type ServerConfig struct {
	HTTPURL      string `yaml:"http_url"`
	ControllerID string `yaml:"controller_id"`
}

// MQTTConfig holds broker connection settings.
type MQTTConfig struct {
	Broker         string `yaml:"broker"`
	ClientID       string `yaml:"client_id"`
	ConnectTimeout int    `yaml:"connect_timeout"`
	KeepAlive      int    `yaml:"keep_alive"`
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
}

// SensorConfig holds a sensor's identity and I2C parameters.
type SensorConfig struct {
	ID         string `yaml:"id"`
	Type       string `yaml:"type"`
	I2CBus     string `yaml:"i2c_bus"`
	I2CAddress uint16 `yaml:"i2c_address"`
	Interval   string `yaml:"interval"`
}


// Load reads and validates the YAML config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.MQTT.Broker == "" {
		return fmt.Errorf("mqtt.broker is required")
	}
	if !strings.HasPrefix(c.MQTT.Broker, "tcp://") &&
		!strings.HasPrefix(c.MQTT.Broker, "ssl://") &&
		!strings.HasPrefix(c.MQTT.Broker, "tls://") &&
		!strings.HasPrefix(c.MQTT.Broker, "ws://") &&
		!strings.HasPrefix(c.MQTT.Broker, "wss://") {
		return fmt.Errorf("mqtt.broker scheme must be tcp://, ssl://, tls://, ws://, or wss://")
	}
	if c.MQTT.ClientID == "" {
		return fmt.Errorf("mqtt.client_id is required")
	}

	if c.MQTT.ConnectTimeout == 0 {
		c.MQTT.ConnectTimeout = 10
	}
	if c.MQTT.KeepAlive == 0 {
		c.MQTT.KeepAlive = 30
	}

	for i, s := range c.Sensors {
		if s.ID == "" {
			return fmt.Errorf("sensors[%d].id is required", i)
		}
		if s.Type == "" {
			return fmt.Errorf("sensors[%d].type is required", i)
		}
		if _, err := time.ParseDuration(s.Interval); err != nil {
			return fmt.Errorf("sensors[%d].interval %q: %w", i, s.Interval, err)
		}
		if s.I2CAddress < 0x08 || s.I2CAddress > 0x77 {
			return fmt.Errorf("sensors[%d].i2c_address 0x%X: must be 0x08–0x77 (7-bit I2C)", i, s.I2CAddress)
		}
	}


	return nil
}

// ParseInterval returns the sensor interval as a time.Duration.
func (s *SensorConfig) ParseInterval() time.Duration {
	d, _ := time.ParseDuration(s.Interval)
	return d
}
