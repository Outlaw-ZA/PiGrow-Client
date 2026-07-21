package provision

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// pinValidity is the lifetime of a single 6-digit PIN per spec §2.3.
const pinValidity = 5 * time.Minute

// serialPrefix is the spec §2.2 wire prefix; serial mints look like
// "PIGROW-A1B2C3".
const serialPrefix = "PIGROW-"

// serialEntropyBytes gives 6 hex chars after the prefix (3 bytes).
const serialEntropyBytes = 3

// PINState is the rotating claim PIN + its expiry, both in the forms
// the spec embeds in the beacon payload.
type PINState struct {
	Pin         string
	ExpiresAtMs int64
}

// Pin holds the rotating claim PIN, the persistent device serial,
// and the rotation timer. It is safe for concurrent use.
type Pin struct {
	mu       sync.Mutex
	pin      string
	expireAt time.Time
	serial   string
}

// NewPin loads or mints the persistent device serial (saved to
// serialPath so it survives reboots) and returns a Pin ready to
// generate the first claim PIN. The first PIN is not generated until
// Current() is called.
func NewPin(serialPath string) (*Pin, error) {
	serial, err := loadOrMintSerial(serialPath)
	if err != nil {
		return nil, err
	}
	return &Pin{serial: serial}, nil
}

// Serial returns the persistent device serial (no internal lock — the
// value is set at construction and never mutated).
func (p *Pin) Serial() string { return p.serial }

// Current returns the currently-valid PIN, minting a fresh one if
// the previous has expired.
func (p *Pin) Current() PINState {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.pin == "" || !now.Before(p.expireAt) {
		p.rotateLocked(now)
	}
	return PINState{Pin: p.pin, ExpiresAtMs: p.expireAt.UnixMilli()}
}

// Rotate forces a new PIN immediately, regardless of remaining
// validity. Used when the Pi wants to invalidate a leaked PIN.
func (p *Pin) Rotate() PINState {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rotateLocked(time.Now())
	return PINState{Pin: p.pin, ExpiresAtMs: p.expireAt.UnixMilli()}
}

// Invalidate clears the active PIN without minting a new one — used
// after a successful claim so any further claim attempt against the
// stale PIN fails. Spec §2.3.
func (p *Pin) Invalidate() {
	p.mu.Lock()
	p.pin = ""
	p.expireAt = time.Time{}
	p.mu.Unlock()
}

// rotateLocked mints and stores a fresh PIN. Caller must hold p.mu.
func (p *Pin) rotateLocked(now time.Time) {
	p.pin = mintPIN()
	p.expireAt = now.Add(pinValidity)
	slog.Info("PIN rotated", "expiresAt", p.expireAt.UnixMilli())
}

// mintPIN returns a 6-digit zero-padded ASCII string from crypto/rand.
func mintPIN() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is exceptional; fallback to time-based seed
		// would be deterministic, so panic is the only honest response.
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return fmt.Sprintf("%06d", n%1000000)
}

// loadOrMintSerial reads serialPath; if missing, mints a fresh serial
// (PIGROW-XXXXXXXX where XXXXXXXX is upperhex from crypto/rand) and
// persists it. Idempotent across boots.
func loadOrMintSerial(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(data))
		if strings.HasPrefix(s, serialPrefix) && len(s) == len(serialPrefix)+serialEntropyBytes*2 {
			return s, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read serial %s: %w", path, err)
	}
	s, err := mintSerial()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(s+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("persist serial %s: %w", path, err)
	}
	return s, nil
}

func mintSerial() (string, error) {
	var b [serialEntropyBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("crypto/rand for serial: %w", err)
	}
	return serialPrefix + strings.ToUpper(hex.EncodeToString(b[:])), nil
}
