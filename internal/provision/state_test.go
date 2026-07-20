package provision

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadActiveStateMissingReturnsNil(t *testing.T) {
	s, err := LoadActiveState(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing-file read: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil for missing file, got %+v", s)
	}
}

func TestSaveAndLoadActiveState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := &ActiveState{
		ProvisionState: "ACTIVE",
		ControllerID:   "abc-123",
		ControllerMAC:  "AA:BB:CC:DD:EE:FF",
		MQTTBrokerURL:  "tcp://192.168.1.10:1883",
		MQTTUsername:   "pigrow-abc-123",
		MQTTPassword:   "shhh",
		ServerHTTPURL:  "http://192.168.1.10:3000",
		PairedAt:       1737000000000,
	}
	if err := SaveActiveState(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadActiveState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil || got.ControllerID != "abc-123" || got.ProvisionState != "ACTIVE" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestActiveStateIsClaimed(t *testing.T) {
	cases := []struct {
		name string
		in   *ActiveState
		want bool
	}{
		{"nil", nil, false},
		{"empty", &ActiveState{}, false},
		{"wrongState", &ActiveState{ControllerID: "x", ProvisionState: "PENDING"}, false},
		{"noControllerID", &ActiveState{ProvisionState: "ACTIVE"}, false},
		{"activeWithID", &ActiveState{ProvisionState: "ACTIVE", ControllerID: "x"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.IsClaimed(); got != c.want {
				t.Errorf("IsClaimed(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestSaveAtomicRename(t *testing.T) {
	// A pre-existing temp file must not block the rename; rename
	// replaces the destination atomically on POSIX.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveActiveState(path, &ActiveState{ControllerID: "x", ProvisionState: "ACTIVE"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be renamed away, stat err=%v", err)
	}
}
