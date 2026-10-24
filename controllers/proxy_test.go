package controllers

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophish/gophish/config"
)

// seenRemoteAddr runs a request through trustedProxyHeaders and returns the
// RemoteAddr host the downstream handler (the rate limiter and audit log)
// would observe.
func seenRemoteAddr(trusted []*net.IPNet, remoteAddr, xff string) string {
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		seen = host
	})
	h := trustedProxyHeaders(trusted, next)
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return seen
}

func mustParse(t *testing.T, entries ...string) []*net.IPNet {
	t.Helper()
	nets, err := config.ParseTrustedProxies(entries)
	if err != nil {
		t.Fatalf("parsing %v: %v", entries, err)
	}
	return nets
}

func TestUntrustedPeerCannotSpoofIP(t *testing.T) {
	trusted := mustParse(t, "10.0.0.0/8")
	// An attacker connecting directly (not via a trusted proxy) sends a
	// forged X-Forwarded-For. It must be ignored, so the limiter/audit see
	// the real peer. A different forged header each time doesn't change that.
	for _, forged := range []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"} {
		if got := seenRemoteAddr(trusted, "203.0.113.9:40000", forged); got != "203.0.113.9" {
			t.Fatalf("spoofed XFF %q was honored: downstream saw %q", forged, got)
		}
	}
}

func TestTrustedProxyHeaderHonored(t *testing.T) {
	trusted := mustParse(t, "10.0.0.0/8")
	// A request from a trusted proxy has its forwarded-for honored.
	if got := seenRemoteAddr(trusted, "10.1.2.3:55555", "198.51.100.7"); got != "198.51.100.7" {
		t.Fatalf("trusted proxy XFF not honored: downstream saw %q", got)
	}
}

func TestNoTrustedProxiesIgnoresHeaders(t *testing.T) {
	// With nothing trusted, even a private-range peer's header is ignored.
	if got := seenRemoteAddr(nil, "10.1.2.3:55555", "198.51.100.7"); got != "10.1.2.3" {
		t.Fatalf("expected peer IP with no trusted proxies, got %q", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	nets, err := config.ParseTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1", "::1", "2001:db8::/32", "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 4 {
		t.Fatalf("expected 4 networks, got %d", len(nets))
	}
	// Bare IPv4 becomes /32, bare IPv6 becomes /128.
	if ones, _ := nets[1].Mask.Size(); ones != 32 {
		t.Fatalf("expected /32 for bare IPv4, got /%d", ones)
	}
	if ones, _ := nets[2].Mask.Size(); ones != 128 {
		t.Fatalf("expected /128 for bare IPv6, got /%d", ones)
	}
	if _, err := config.ParseTrustedProxies([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected an error for an invalid entry")
	}
}
