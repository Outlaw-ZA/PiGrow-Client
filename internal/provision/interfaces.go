package provision

import (
	"fmt"
	"net"
	"strings"
)

// PrimaryInterface bundles the network identity the Pi sends in its
// beacon: MAC (colon-separated uppercase, spec §2.2) and the IPv4
// address of the interface the beacon came from. The MAC is the
// Controller upsert key on the server side.
type PrimaryInterface struct {
	MAC string
	IP  string
}

// PickPrimaryInterface returns the first non-loopback, up network
// interface's MAC + first IPv4 address. Both are best-effort: tests
// and chrooted environments can call this and accept the empty
// fallback on failure.
func PickPrimaryInterface() (*PrimaryInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Skip interfaces without a hardware addr (e.g. lo, tun).
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		mac := FormatMAC(iface.HardwareAddr)

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			v4 := ip.To4()
			if v4 == nil {
				continue
			}
			return &PrimaryInterface{MAC: mac, IP: v4.String()}, nil
		}
	}
	return nil, fmt.Errorf("no suitable primary interface found")
}

// FormatMAC converts raw 6-byte MAC into the spec §2.2 wire form:
// colon-separated uppercase hex (AA:BB:CC:DD:EE:FF).
func FormatMAC(hw net.HardwareAddr) string {
	b := []byte(hw)
	out := make([]string, len(b))
	for i, x := range b {
		const hexDigits = "0123456789ABCDEF"
		out[i] = string([]byte{hexDigits[x>>4], hexDigits[x&0x0F]})
	}
	return strings.Join(out, ":")
}

// FormatMACString parses a colon/dash separated MAC (upper or lower case)
// and returns the canonical uppercase-colon form. Useful when comparing
// against a ClaimResponse.controllerMac.
func FormatMACString(s string) (string, error) {
	raw, err := net.ParseMAC(s)
	if err != nil {
		return "", err
	}
	return FormatMAC(raw), nil
}
