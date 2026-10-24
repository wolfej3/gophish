package controllers

import (
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

// TestPasswordChangeRequiredBlocksSessionAPI ensures a session can't drive the
// API while a password change is required (CVE-2026-82269).
func TestPasswordChangeRequiredBlocksSessionAPI(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := loginJar(t, ctx)
	// Access works before the flag.
	if resp := apiReq(t, ctx, client, http.MethodGet, "/api/campaigns/", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 before the flag, got %d", resp.StatusCode)
	}
	u, _ := models.GetUser(1)
	u.PasswordChangeRequired = true
	models.PutUser(&u)
	if resp := apiReq(t, ctx, client, http.MethodGet, "/api/campaigns/", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 while password change is required, got %d", resp.StatusCode)
	}
}
