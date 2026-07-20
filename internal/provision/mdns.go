package provision

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

// mdnsServiceType is the spec §2.1 DNS-SD service type. The trailing
// period is required by the FQDN validator inside hashicorp/mdns.
const mdnsServiceType = "_pigrow._tcp."

// mdnsDomain is the multicast DNS domain. Required trailing dot.
const mdnsDomain = "local."

// MDNSAdvertiser announces the Pi's claim eligibility on the LAN via
// DNS-SD service _pigrow._tcp.local. (spec §2.1).
type MDNSAdvertiser struct {
	Serial    string
	OwnMAC    string
	IP        string
	FwVersion string
	Pin       *Pin
}

// NewMDNSAdvertiser wires an advertiser from the running beacon state.
func NewMDNSAdvertiser(serial, mac, ip, fw string, pin *Pin) *MDNSAdvertiser {
	return &MDNSAdvertiser{
		Serial:    serial,
		OwnMAC:    mac,
		IP:        ip,
		FwVersion: fw,
		Pin:       pin,
	}
}

// Start spins up an mDNS responder advertising the Pi's claim state
// until ctx is cancelled. Errors are logged but non-fatal — the UDP
// beacon is the primary discovery channel (spec §2.1 / deliberate-
// design invariant (a)). The hashicorp/mdns library does not expose
// in-place TXT record mutation, so the service is constructed once
// with the initial PIN/exp and stays for the life of the advertiser;
// PIN rotation propagates only via the next beacon cycle.
func (m *MDNSAdvertiser) Start(ctx context.Context) (shutdown func(), err error) {
	if m.Pin == nil {
		return nil, fmt.Errorf("mdns advertiser: nil Pin")
	}
	if m.Serial == "" {
		return nil, fmt.Errorf("mdns advertiser: empty serial")
	}
	if m.OwnMAC == "" {
		return nil, fmt.Errorf("mdns advertiser: empty MAC")
	}
	if m.IP == "" {
		return nil, fmt.Errorf("mdns advertiser: empty IP")
	}

	hostname := strings.ToLower(m.Serial) + "." + mdnsDomain
	ip := net.ParseIP(m.IP)
	if ip == nil {
		return nil, fmt.Errorf("mdns advertiser: invalid IP %q", m.IP)
	}
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("mdns advertiser: only IPv4 supported; got %s", m.IP)
	}

	svc, err := mdns.NewMDNSService(
		strings.ToLower(m.Serial),
		mdnsServiceType,
		mdnsDomain,
		hostname,
		0, // port: v1 is presence-only, no listening service
		[]net.IP{v4},
		m.buildTXT(),
	)
	if err != nil {
		return nil, fmt.Errorf("mdns NewMDNSService: %w", err)
	}

	srv, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		return nil, fmt.Errorf("mdns NewServer: %w", err)
	}
	slog.Info("mDNS advertiser running", "host", hostname, "service", mdnsServiceType)

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		srv.Shutdown()
		close(done)
	}()
	return func() { <-done }, nil
}

// buildTXT assembles the §2.1 TXT array (pgv, pgserial, pgmac, pgpin,
// pgexp, pgfw, pgbeacon). pgbeacon is base64url of compact JSON so a
// server that only saw mDNS still has the full §2.2 payload.
func (m *MDNSAdvertiser) buildTXT() []string {
	pin := m.Pin.Current()
	raw := BuildBeacon(time.Now(), &PrimaryInterface{MAC: m.OwnMAC, IP: m.IP}, m.Serial, m.FwVersion, pin, nil)
	beacon := base64.RawURLEncoding.EncodeToString(raw)
	return []string{
		fmt.Sprintf("pgv=%d", 1),
		fmt.Sprintf("pgserial=%s", m.Serial),
		fmt.Sprintf("pgmac=%s", m.OwnMAC),
		fmt.Sprintf("pgpin=%s", pin.Pin),
		fmt.Sprintf("pgexp=%d", pin.ExpiresAtMs),
		fmt.Sprintf("pgfw=%s", m.FwVersion),
		fmt.Sprintf("pgbeacon=%s", beacon),
	}
}
