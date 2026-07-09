package device

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
)

// RPIDevice controls a GPIO-driven device on a Raspberry Pi.
type RPIDevice struct {
	id       string
	mu       sync.Mutex
	pinCache map[int]gpio.PinIO
}

// NewRPIDevice creates a new Raspberry Pi GPIO device.
func NewRPIDevice(id string) *RPIDevice {
	return &RPIDevice{
		id:       id,
		pinCache: make(map[int]gpio.PinIO),
	}
}

// ID returns the device's configured identifier.
func (d *RPIDevice) ID() string { return d.id }

// On sets the given GPIO pin HIGH.
func (d *RPIDevice) On(pin int) error {
	return d.setPin(pin, gpio.High)
}

// Off sets the given GPIO pin LOW.
func (d *RPIDevice) Off(pin int) error {
	return d.setPin(pin, gpio.Low)
}

func (d *RPIDevice) setPin(pin int, level gpio.Level) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	p, ok := d.pinCache[pin]
	if !ok {
		p = gpioreg.ByName(strconv.Itoa(pin))
		if p == nil {
			return fmt.Errorf("gpio pin %d not found", pin)
		}
		d.pinCache[pin] = p
	}
	if err := p.Out(level); err != nil {
		return fmt.Errorf("gpio pin %d set %v: %w", pin, level, err)
	}
	slog.Info("GPIO set", "device", d.id, "pin", pin, "level", level)
	return nil
}

// Close releases any resources. For RPI via periph this is a no-op
// (pins are released when the host driver shuts down).
func (d *RPIDevice) Close() error {
	return nil
}

// Compile-time interface check.
var _ Device = (*RPIDevice)(nil)
