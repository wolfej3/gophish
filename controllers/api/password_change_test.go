package api

import (
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

// TestPasswordChangeRequiredBlocksAPIKeys ensures a user flagged to change
// their password can't use the full or scoped API key (CVE-2026-82269).
func TestPasswordChangeRequiredBlocksAPIKeys(t *testing.T) {
	testCtx := setupTest(t)
	// Create a scoped key before flagging the account.
	scoped := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "k", Access: "read"})

	u, _ := models.GetUser(1)
	u.PasswordChangeRequired = true
	if err := models.PutUser(&u); err != nil {
		t.Fatalf("error flagging user: %v", err)
	}

	for _, key := range []string{testCtx.apiKey, scoped} {
		w := apiRequest(t, testCtx, http.MethodGet, "/api/campaigns/", key, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 while password change is required, got %d", w.Code)
		}
	}

	// Clearing the flag restores access.
	u.PasswordChangeRequired = false
	models.PutUser(&u)
	if w := apiRequest(t, testCtx, http.MethodGet, "/api/campaigns/", testCtx.apiKey, nil); w.Code != http.StatusOK {
		t.Fatalf("expected access after clearing the flag, got %d", w.Code)
	}
}
