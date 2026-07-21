package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMinimalConfig(t *testing.T) {
	yaml := `
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "test-client"
sensors:
  - id: "test-sensor-1"
    type: "TEMP_HUMIDITY"
    i2c_bus: "/dev/i2c-1"
    i2c_address: 0x44
    interval: "30s"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.MQTT.Broker != "tcp://localhost:1883" {
		t.Errorf("expected broker tcp://localhost:1883, got %s", cfg.MQTT.Broker)
	}
	if cfg.MQTT.ClientID != "test-client" {
		t.Errorf("expected client_id test-client, got %s", cfg.MQTT.ClientID)
	}
	if len(cfg.Sensors) != 1 {
		t.Fatalf("expected 1 sensor, got %d", len(cfg.Sensors))
	}
	if cfg.Sensors[0].Type != "TEMP_HUMIDITY" {
		t.Errorf("expected sensor type TEMP_HUMIDITY, got %s", cfg.Sensors[0].Type)
	}
}

func TestLoadServerConfig(t *testing.T) {
	yaml := `
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "test-client"
server:
  http_url: "http://localhost:4000/api"
  controller_id: "abc-123"
sensors:
  - id: "test-sensor-1"
    type: "TEMP_HUMIDITY"
    i2c_bus: "/dev/i2c-1"
    i2c_address: 0x44
    interval: "30s"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Server.HTTPURL != "http://localhost:4000/api" {
		t.Errorf("expected http_url http://localhost:4000/api, got %s", cfg.Server.HTTPURL)
	}
	if cfg.Server.ControllerID != "abc-123" {
		t.Errorf("expected controller_id abc-123, got %s", cfg.Server.ControllerID)
	}
}

func TestLoadMissingBroker(t *testing.T) {
	yaml := `
mqtt:
  client_id: "test-client"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for missing broker, got nil")
	}
}

func TestLoadMissingClientID(t *testing.T) {
	yaml := `
mqtt:
  broker: "tcp://localhost:1883"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for missing client_id, got nil")
	}
}

func TestLoadInvalidBrokerScheme(t *testing.T) {
	yaml := `
mqtt:
  broker: "http://localhost:1883"
  client_id: "test-client"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for invalid broker scheme, got nil")
	}
}

func TestLoadNoSensorsOrDevices(t *testing.T) {
	yaml := `
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "test-client"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("expected no error for MQTT-only config, got: %v", err)
	}
	if cfg.MQTT.ConnectTimeout != 10 {
		t.Errorf("expected default connect_timeout 10, got %d", cfg.MQTT.ConnectTimeout)
	}
	if cfg.MQTT.KeepAlive != 30 {
		t.Errorf("expected default keep_alive 30, got %d", cfg.MQTT.KeepAlive)
	}
}

func TestLoadInvalidI2CAddress(t *testing.T) {
	yaml := `
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "test-client"
sensors:
  - id: "test-sensor-1"
    type: "TEMP_HUMIDITY"
    i2c_bus: "/dev/i2c-1"
    i2c_address: 0x00
    interval: "30s"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for invalid I2C address, got nil")
	}
}

func TestLoadDefaults(t *testing.T) {
	yaml := `
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "test-client"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.MQTT.ConnectTimeout != 10 {
		t.Errorf("expected default connect_timeout 10, got %d", cfg.MQTT.ConnectTimeout)
	}
	if cfg.MQTT.KeepAlive != 30 {
		t.Errorf("expected default keep_alive 30, got %d", cfg.MQTT.KeepAlive)
	}
}
