package sensor

import (
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"
)

// SHT40 I2C command constants.
const (
	sht40MeasureHighPrecision = 0xFD
	sht40MeasureResponseLen   = 6
	sht40MeasureDelay         = 10 * time.Millisecond
)

// measureCmd is the pre-allocated measurement trigger to avoid per-tick
// allocations.
var measureCmd = [1]byte{sht40MeasureHighPrecision}

// SHT40Sensor implements Sensor for the Sensirion SHT40 temperature/humidity sensor.
type SHT40Sensor struct {
	id       string
	interval time.Duration
	dev      *i2c.Dev
	bus      i2c.BusCloser
}

// NewSHT40 creates a new SHT40 sensor on the given I2C bus and address.
func NewSHT40(id, i2cBus string, i2cAddr uint16, interval time.Duration) (*SHT40Sensor, error) {
	bus, err := openBus(i2cBus)
	if err != nil {
		return nil, fmt.Errorf("sht40 %s: open i2c bus %s: %w", id, i2cBus, err)
	}

	dev := &i2c.Dev{Bus: bus, Addr: i2cAddr}
	slog.Info("SHT40 sensor initialized", "id", id, "bus", i2cBus, "addr", fmt.Sprintf("0x%X", i2cAddr))

	return &SHT40Sensor{
		id:       id,
		interval: interval,
		dev:      dev,
		bus:      bus,
	}, nil
}

// InitHost initialises the periph host drivers. Must be called once before using any periph I/O.
func InitHost() error {
	_, err := host.Init()
	return err
}

func openBus(path string) (i2c.BusCloser, error) {
	// Convert filesystem path like "/dev/i2c-1" to periph bus name "I2C1".
	name := strings.TrimPrefix(path, "/dev/i2c-")
	return i2creg.Open("I2C" + name)
}

// ID returns the sensor's configured identifier.
func (s *SHT40Sensor) ID() string { return s.id }

// Type returns the sensor type.
func (s *SHT40Sensor) Type() string { return "TEMP_HUMIDITY" }

// Interval returns the configured read interval.
func (s *SHT40Sensor) Interval() time.Duration { return s.interval }

// Read performs a high-precision measurement on the SHT40 and validates the
// per-reading CRC-8 checksums (polynomial 0x31, init 0xFF) provided by the
// sensor. Returns both TEMPERATURE and HUMIDITY readings.
func (s *SHT40Sensor) Read() ([]Reading, error) {
	if err := s.triggerMeasurement(); err != nil {
		return nil, err
	}

	time.Sleep(sht40MeasureDelay)

	var data [sht40MeasureResponseLen]byte
	if err := s.dev.Tx(nil, data[:]); err != nil {
		return nil, fmt.Errorf("sht40 %s: read: %w", s.id, err)
	}

	// CRC-8 validation per the SHT40 datasheet: temperature CRC covers
	// bytes [0:2], humidity CRC covers bytes [3:5].
	if crc8(data[:2]) != data[2] {
		return nil, fmt.Errorf("sht40 %s: temperature CRC mismatch", s.id)
	}
	if crc8(data[3:5]) != data[5] {
		return nil, fmt.Errorf("sht40 %s: humidity CRC mismatch", s.id)
	}

	rawTemp := uint16(data[0])<<8 | uint16(data[1])
	rawHum := uint16(data[3])<<8 | uint16(data[4])

	temperature := roundTo1(-45.0 + 175.0*float64(rawTemp)/65535.0)
	humidity := roundTo1(-6.0 + 125.0*float64(rawHum)/65535.0)
	humidity = clamp(humidity, 0, 100)

	return []Reading{
		{SensorType: "TEMPERATURE", Value: temperature},
		{SensorType: "HUMIDITY", Value: humidity},
	}, nil
}

func (s *SHT40Sensor) triggerMeasurement() error {
	return s.dev.Tx(measureCmd[:], nil)
}

// crc8 computes the CRC-8 checksum used by the SHT40 (polynomial 0x31,
// initialization 0xFF, no reflection, no final XOR).
func crc8(data []byte) byte {
	crc := byte(0xFF)
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x31
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func roundTo1(v float64) float64 {
	return math.Round(v*10) / 10
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Compile-time interface check.
var _ Sensor = (*SHT40Sensor)(nil)
