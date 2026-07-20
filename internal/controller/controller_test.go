package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/device"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/mqtt"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/sensor"
)

// mockMQTT records published messages for test inspection.
type mockMQTT struct {
	mu         sync.Mutex
	published  []pubEntry
	subscribed []subEntry
}

type pubEntry struct {
	topic   string
	payload []byte
}

type subEntry struct {
	topic   string
	handler mqtt.MessageHandler
}

func (m *mockMQTT) Publish(topic string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, pubEntry{topic, payload})
	return nil
}

func (m *mockMQTT) PublishQoS0(topic string, payload []byte) error {
	return m.Publish(topic, payload)
}

func (m *mockMQTT) Subscribe(topic string, handler mqtt.MessageHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribed = append(m.subscribed, subEntry{topic, handler})
	return nil
}

func (m *mockMQTT) Disconnect() {}

func (m *mockMQTT) publishedOn(topic string) []pubEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []pubEntry
	for _, p := range m.published {
		if p.topic == topic {
			out = append(out, p)
		}
	}
	return out
}

// compile-time check that mockMQTT satisfies mqtt.Client.
var _ mqtt.Client = (*mockMQTT)(nil)

// mockDevice records On/Off calls.
type mockDevice struct {
	id   string
	onFn func(int) error
}

func (d *mockDevice) On(pin int) error {
	if d.onFn != nil {
		return d.onFn(pin)
	}
	return nil
}
func (d *mockDevice) Off(pin int) error { return nil }
func (d *mockDevice) ID() string        { return d.id }
func (d *mockDevice) Close() error      { return nil }

// mockSensor returns fixed readings.
type mockSensor struct {
	id       string
	readings []sensor.Reading
	interval time.Duration
}

func (s *mockSensor) Read() ([]sensor.Reading, error) { return s.readings, nil }
func (s *mockSensor) ID() string                       { return s.id }
func (s *mockSensor) Type() string                     { return "TEMP_HUMIDITY" }
func (s *mockSensor) Interval() time.Duration          { return s.interval }

func TestControllerSubscribesToCommands(t *testing.T) {
	mock := &mockMQTT{}
	var wg sync.WaitGroup
	ctrl := Controller{
		cfg:        &config.Config{},
		mqttClient: mock,
		sensors:    []sensor.Sensor{},
		deviceMap:  map[string]device.Device{},
		cmdCh:      make(chan cmdJob, 64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ctrl.Start(ctx, &wg)
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	mock.mu.Lock()
	subCount := len(mock.subscribed)
	mock.mu.Unlock()
	if subCount != 1 {
		t.Fatalf("expected 1 subscription, got %d", subCount)
	}
	if mock.subscribed[0].topic != "devices/+/commands" {
		t.Errorf("expected topic devices/+/commands, got %s", mock.subscribed[0].topic)
	}
}

func TestControllerSubscribesWithDevices(t *testing.T) {
	mock := &mockMQTT{}
	var wg sync.WaitGroup
	ctrl := Controller{
		cfg:        &config.Config{},
		mqttClient: mock,
		sensors:    []sensor.Sensor{},
		deviceMap:  map[string]device.Device{"dev-1": &mockDevice{id: "dev-1"}},
		cmdCh:      make(chan cmdJob, 64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ctrl.Start(ctx, &wg)
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	mock.mu.Lock()
	subCount := len(mock.subscribed)
	mock.mu.Unlock()
	if subCount != 1 {
		t.Fatalf("expected 1 subscription, got %d", subCount)
	}
	if mock.subscribed[0].topic != "devices/+/commands" {
		t.Errorf("expected topic devices/+/commands, got %s", mock.subscribed[0].topic)
	}
}

func TestSensorPublishesTelemetry(t *testing.T) {
	mock := &mockMQTT{}
	var wg sync.WaitGroup
	ctrl := Controller{
		cfg:        &config.Config{},
		mqttClient: mock,
		sensors: []sensor.Sensor{
			&mockSensor{
				id:       "sensor-1",
				readings: []sensor.Reading{{SensorType: "TEMPERATURE", Value: 25.5}},
				interval: 10 * time.Millisecond,
			},
		},
		deviceMap:  map[string]device.Device{},
		cmdCh:      make(chan cmdJob, 64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ctrl.Start(ctx, &wg)
	time.Sleep(60 * time.Millisecond)
	cancel()
	wg.Wait()

	entries := mock.publishedOn("sensors/sensor-1/telemetry")
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 telemetry publish, got %d", len(entries))
	}
	if len(entries) > 3 {
		t.Logf("note: published %d times in 60ms (interval 10ms) — acceptable", len(entries))
	}
}

func TestDeviceCommandExecutesAndPublishesState(t *testing.T) {
	var executed atomic.Bool
	mock := &mockMQTT{}
	ctrl := Controller{
		cfg:        &config.Config{},
		mqttClient: mock,
		sensors:    []sensor.Sensor{},
		deviceMap: map[string]device.Device{
			"dev-1": &mockDevice{
				id: "dev-1",
				onFn: func(pin int) error {
					executed.Store(true)
					return nil
				},
			},
		},
		cmdCh:      make(chan cmdJob, 64),
	}

	// Start the command worker to process the channel.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go ctrl.commandWorker(ctx, &wg)

	ctrl.deviceCommandHandler("devices/dev-1/commands", []byte(`{"action":"ON","pin":17,"timestamp":1000}`))
	time.Sleep(20 * time.Millisecond)

	cancel()
	wg.Wait()

	if !executed.Load() {
		t.Error("expected device.On() to be called")
	}
	stateEntries := mock.publishedOn("devices/dev-1/state")
	if len(stateEntries) < 1 {
		t.Fatal("expected state publish after command execution")
	}
}
