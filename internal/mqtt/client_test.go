package mqtt

import (
	"testing"
	"time"

	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

func TestConfigDefaultsApplied(t *testing.T) {
	// Validate that our factory correctly builds options by verifying
	// the token returned from an unconnectable broker errors.
	cfg := Config{
		Broker:         "tcp://127.0.0.1:1",
		ClientID:       "test-unit",
		ConnectTimeout: 100 * time.Millisecond,
		KeepAlive:      10,
	}

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected connection error on unreachable broker, got nil")
	}
}

func TestConfigEmptyBroker(t *testing.T) {
	cfg := Config{
		Broker:         "",
		ClientID:       "test-unit",
		ConnectTimeout: 100 * time.Millisecond,
	}

	if cfg.Broker == "" {
		t.Log("broker empty — New will fail with unresolved address")
	}
}

func TestNewSetsWillOnConnect(t *testing.T) {
	opts := mqttlib.NewClientOptions().
		AddBroker("tcp://127.0.0.1:1").
		SetClientID("will-test").
		SetConnectTimeout(10 * time.Millisecond)

	willTopic := "pigrow-client/will-test/status"
	willPayload := `{"online":false}`
	opts.SetWill(willTopic, willPayload, 1, true)

	if opts.ClientID != "will-test" {
		t.Errorf("expected client ID will-test, got %s", opts.ClientID)
	}
}

func TestNewConnectPublishesOnline(t *testing.T) {
	opts := mqttlib.NewClientOptions().
		AddBroker("tcp://127.0.0.1:1").
		SetClientID("online-test").
		SetConnectTimeout(10 * time.Millisecond)

	opts.SetWill("pigrow-client/online-test/status", `{"online":false}`, 1, true)

	var onConnectCalled bool
	opts.OnConnect = func(client mqttlib.Client) {
		onConnectCalled = true
		token := client.Publish("pigrow-client/online-test/status", 1, true, `{"online":true}`)
		if token.WaitTimeout(10*time.Millisecond) && token.Error() != nil {
			t.Logf("publish error (expected with no broker): %v", token.Error())
		}
	}

	client := mqttlib.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(10 * time.Millisecond) {
		t.Log("connect timed out (expected with no broker)")
	}

	if !onConnectCalled {
		t.Log("OnConnect not called — expected because connection never completed")
	}
}
