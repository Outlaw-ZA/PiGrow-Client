package provision

import (
	"testing"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
)

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
