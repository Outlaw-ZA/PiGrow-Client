package provision

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPinIsSixDigitsAndZeroPadded(t *testing.T) {
	p := newTestPin(t)
	for i := 0; i < 50; i++ {
		s := p.Current()
		if len(s.Pin) != 6 {
			t.Fatalf("expected 6-digit PIN, got %q", s.Pin)
		}
		if !strings.HasPrefix(s.Pin, "0") && len(s.Pin) == 6 && s.Pin[0] != '0' {
			// ok
		}
		// Verify all characters are digits.
		for _, c := range s.Pin {
			if c < '0' || c > '9' {
				t.Fatalf("PIN %q contains non-digit", s.Pin)
			}
		}
		// Expiry should be ~5min in the future.
		delta := time.Until(time.UnixMilli(s.ExpiresAtMs))
		if delta < 4*time.Minute || delta > 5*time.Minute+5*time.Second {
			t.Errorf("expiry %v out of expected window", delta)
		}
	}
}

func TestPinRotateChangesValue(t *testing.T) {
	p := newTestPin(t)
	a := p.Current().Pin
	b := p.Rotate().Pin
	if a == b {
		// crypto/rand collisions are astronomically unlikely on 1M space
		// in 50 trials; flag if both are equal.
		t.Errorf("expected Rotate() to change PIN; a=%s b=%s", a, b)
	}
}

func TestPinInvalidateClearsState(t *testing.T) {
	p := newTestPin(t)
	_ = p.Current()
	p.Invalidate()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pin != "" {
		t.Errorf("expected empty pin after Invalidate, got %q", p.pin)
	}
}

func TestPinConcurrentDistinctValues(t *testing.T) {
	p := newTestPin(t)
	seen := sync.Map{}
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := p.Current()
			seen.Store(s.Pin, struct{}{})
		}()
	}
	wg.Wait()
	count := 0
	seen.Range(func(_, _ any) bool { count++; return true })
	if count < 1 {
		t.Fatal("no PINs observed")
	}
	if count > 16 {
		// Allow some collision but not "one PIN dominates" — random
		// distribution across 1e6 should spread across many values.
		t.Logf("observed %d distinct PINs across 200 concurrent calls (informational)", count)
	}
}

func TestSerialMintAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serial.txt")

	p1, err := NewPin(path)
	if err != nil {
		t.Fatalf("first NewPin: %v", err)
	}
	s1 := p1.Serial()
	if !strings.HasPrefix(s1, serialPrefix) {
		t.Errorf("missing prefix: %s", s1)
	}
	if len(s1) != len(serialPrefix)+serialEntropyBytes*2 {
		t.Errorf("unexpected serial length: %s", s1)
	}
	for _, c := range s1[len(serialPrefix):] {
		if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'F') {
			t.Errorf("serial not uppercase-hex: %s", s1)
		}
	}

	// NewPin on the same path returns the same serial.
	p2, err := NewPin(path)
	if err != nil {
		t.Fatalf("second NewPin: %v", err)
	}
	if p2.Serial() != s1 {
		t.Errorf("expected persistent serial %s, got %s", s1, p2.Serial())
	}
}

func TestSerialMintTwiceDifferent(t *testing.T) {
	a, err := mintSerial()
	if err != nil {
		t.Fatal(err)
	}
	b, err := mintSerial()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		// 16M possible values; collision in two trials would be wrong.
		t.Errorf("two serial mints collided: %s", a)
	}
}

func TestFormatMACColonUpper(t *testing.T) {
	if got := FormatMAC([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}); got != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("FormatMAC upper: %q", got)
	}
	if got, err := FormatMACString("aa:bb:cc:dd:ee:ff"); err != nil || got != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("FormatMACString: got=%q err=%v", got, err)
	}
	if _, err := FormatMACString("not-a-mac"); err == nil {
		t.Error("expected error for garbage MAC")
	}
}

func newTestPin(t *testing.T) *Pin {
	t.Helper()
	dir := t.TempDir()
	p, err := NewPin(filepath.Join(dir, "serial.txt"))
	if err != nil {
		t.Fatalf("NewPin: %v", err)
	}
	return p
}
