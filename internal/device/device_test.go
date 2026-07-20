package device

import "testing"

type recordingDevice struct {
	id       string
	onCalls  []int
	offCalls []int
}

func (d *recordingDevice) On(pin int) error {
	d.onCalls = append(d.onCalls, pin)
	return nil
}
func (d *recordingDevice) Off(pin int) error {
	d.offCalls = append(d.offCalls, pin)
	return nil
}
func (d *recordingDevice) ID() string   { return d.id }
func (d *recordingDevice) Close() error { return nil }

func TestDeviceInterface(t *testing.T) {
	d := &recordingDevice{id: "test-dev"}

	if d.ID() != "test-dev" {
		t.Errorf("expected ID test-dev, got %s", d.ID())
	}

	if err := d.On(17); err != nil {
		t.Errorf("On failed: %v", err)
	}
	if len(d.onCalls) != 1 || d.onCalls[0] != 17 {
		t.Errorf("expected On(17), got %v", d.onCalls)
	}

	if err := d.Off(17); err != nil {
		t.Errorf("Off failed: %v", err)
	}
	if len(d.offCalls) != 1 || d.offCalls[0] != 17 {
		t.Errorf("expected Off(17), got %v", d.offCalls)
	}

	if err := d.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
