package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gophish/gophish/dialer"
)

// TestImportSiteBlocksSSRF ensures Import Site refuses to fetch internal
// addresses and non-http(s) schemes (CVE-2026-107273).
func TestImportSiteBlocksSSRF(t *testing.T) {
	testCtx := setupTest(t)
	dialer.SetAllowedHosts(nil)

	blocked := []string{
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://localhost/", // resolves to loopback
	}
	for _, u := range blocked {
		w := apiRequest(t, testCtx, http.MethodPost, "/api/import/site", testCtx.apiKey, map[string]string{"url": u})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %s to be refused with 400, got %d %s", u, w.Code, w.Body.String())
		}
	}

	// Non-http(s) schemes are rejected before any fetch.
	for _, u := range []string{"file:///etc/passwd", "gopher://127.0.0.1/", "ftp://example.com/"} {
		w := apiRequest(t, testCtx, http.MethodPost, "/api/import/site", testCtx.apiKey, map[string]string{"url": u})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "http or https") {
			t.Fatalf("expected %s to be rejected for scheme, got %d %s", u, w.Code, w.Body.String())
		}
	}
}
