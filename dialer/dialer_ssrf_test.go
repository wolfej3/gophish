package dialer

import (
	"strings"
	"syscall"
	"testing"
)

// control returns the Control function for a dialer with the given allowlist.
func control(allowed ...string) func(string, string, syscall.RawConn) error {
	d := &RestrictedDialer{}
	if len(allowed) > 0 {
		d.SetAllowedHosts(allowed)
	}
	return d.Dialer().Control
}

func TestDefaultBlocksInternalTargets(t *testing.T) {
	ctrl := control()
	var raw syscall.RawConn
	cases := []struct {
		network string
		addr    string
	}{
		{"tcp4", "127.0.0.1:80"},          // IPv4 loopback
		{"tcp6", "[::1]:80"},              // IPv6 loopback
		{"tcp4", "169.254.169.254:80"},    // cloud metadata
		{"tcp4", "10.0.0.1:80"},           // RFC1918
		{"tcp4", "192.168.1.5:80"},        // RFC1918
		{"tcp4", "100.64.0.1:80"},         // CGNAT
		{"tcp4", "0.0.0.0:80"},            // unspecified
		{"tcp6", "[::ffff:127.0.0.1]:80"}, // IPv4-mapped loopback
		{"tcp6", "[fe80::1]:80"},          // IPv6 link-local
		{"tcp6", "[fc00::1]:80"},          // IPv6 ULA
	}
	for _, c := range cases {
		err := ctrl(c.network, c.addr, raw)
		if err == nil || !strings.Contains(err.Error(), "denied") {
			t.Fatalf("expected %s to be denied, got %v", c.addr, err)
		}
	}
}

func TestDefaultAllowsPublic(t *testing.T) {
	ctrl := control()
	var raw syscall.RawConn
	for _, addr := range []string{"1.1.1.1:443", "8.8.8.8:80", "93.184.216.34:443"} {
		if err := ctrl("tcp4", addr, raw); err != nil {
			t.Fatalf("expected %s to be allowed, got %v", addr, err)
		}
	}
}

func TestAllowlistOptsInternalBackIn(t *testing.T) {
	ctrl := control("10.1.2.3")
	var raw syscall.RawConn
	if err := ctrl("tcp4", "10.1.2.3:80", raw); err != nil {
		t.Fatalf("expected the allowlisted host to pass, got %v", err)
	}
	// A different internal host is still denied.
	if err := ctrl("tcp4", "10.1.2.4:80", raw); err == nil {
		t.Fatal("expected a non-allowlisted internal host to be denied")
	}
}
