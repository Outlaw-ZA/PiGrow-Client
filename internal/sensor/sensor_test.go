package sensor

import (
	"encoding/json"
	"testing"
	"time"
)

type staticSensor struct {
	id       string
	readings []Reading
	typ      string
	interval time.Duration
}

func (s *staticSensor) Read() ([]Reading, error) { return s.readings, nil }
func (s *staticSensor) ID() string               { return s.id }
func (s *staticSensor) Type() string             { return s.typ }
func (s *staticSensor) Interval() time.Duration  { return s.interval }

func TestSensorInterface(t *testing.T) {
	s := &staticSensor{
		id:       "test-sensor",
		readings: []Reading{{SensorType: "TEMPERATURE", Value: 25.5}},
		typ:      "TEMP_HUMIDITY",
		interval: 30 * time.Second,
	}

	if s.ID() != "test-sensor" {
		t.Errorf("expected ID test-sensor, got %s", s.ID())
	}
	if s.Type() != "TEMP_HUMIDITY" {
		t.Errorf("expected type TEMP_HUMIDITY, got %s", s.Type())
	}
	if s.Interval() != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", s.Interval())
	}

	readings, err := s.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(readings))
	}
	if readings[0].SensorType != "TEMPERATURE" || readings[0].Value != 25.5 {
		t.Errorf("expected TEMPERATURE 25.5, got %s %f", readings[0].SensorType, readings[0].Value)
	}
}

func TestReadingJSON(t *testing.T) {
	r := Reading{SensorType: "HUMIDITY", Value: 60.1}
	expected := `{"sensorType":"HUMIDITY","value":60.1}`
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}
