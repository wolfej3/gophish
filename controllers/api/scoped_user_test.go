package api

import (
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

// TestScopedKeyCannotChangeUserPassword ensures a limited (gpk_) key can't
// change a user's password (or otherwise manage accounts), which would let it
// take over the owner's account.
func TestScopedKeyCannotChangeUserPassword(t *testing.T) {
	testCtx := setupTest(t)
	// A scoped key with full write access, including the users resource.
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "k", Access: "write"})

	// Try to change the admin's (its owner's) password.
	body := userRequest{Username: "admin", Role: models.RoleAdmin, Password: "attackerpass"}
	w := apiRequest(t, testCtx, http.MethodPut, "/api/users/1", key, body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected a scoped key to be forbidden from changing a password, got %d %s", w.Code, w.Body.String())
	}
	// Creating a user is also forbidden.
	if w := apiRequest(t, testCtx, http.MethodPost, "/api/users/", key, userRequest{Username: "new", Role: models.RoleAdmin, Password: "longpassword"}); w.Code != http.StatusForbidden {
		t.Fatalf("expected a scoped key to be forbidden from creating users, got %d", w.Code)
	}

	// The full key can still manage users. User 1 is the provider admin, so we
	// keep that role rather than demoting the only provider admin.
	if w := apiRequest(t, testCtx, http.MethodPut, "/api/users/1", testCtx.apiKey, userRequest{Username: "admin", Role: models.RoleProviderAdmin, Password: "newadminpass"}); w.Code != http.StatusOK {
		t.Fatalf("expected the full key to change the password, got %d %s", w.Code, w.Body.String())
	}
}
