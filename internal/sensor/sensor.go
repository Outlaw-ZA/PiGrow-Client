package sensor

import "time"

// Reading represents a single sensor measurement.
type Reading struct {
	SensorType string  `json:"sensorType"`
	Value      float64 `json:"value"`
}

// Sensor is the interface every sensor driver must implement.
type Sensor interface {
	Read() ([]Reading, error)
	ID() string
	Type() string
	Interval() time.Duration
}
