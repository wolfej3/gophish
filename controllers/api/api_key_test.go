package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophish/gophish/models"
)

// createScopedKey creates a scoped API key through the API with the given
// full-access key, returning the new key.
func createScopedKey(t *testing.T, testCtx *testContext, apiKey string, req apiKeyRequest) string {
	t.Helper()
	w := apiRequest(t, testCtx, http.MethodPost, "/api/api_keys/", apiKey, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("unexpected status creating API key: %d %s", w.Code, w.Body.String())
	}
	resp := apiKeyResponse{}
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.HasPrefix(resp.Key, models.ScopedAPIKeyPrefix) || resp.APIKey.Name != req.Name {
		t.Fatalf("unexpected response creating API key: %+v", resp)
	}
	return resp.Key
}

func expectStatus(t *testing.T, testCtx *testContext, method, path, apiKey string, body interface{}, status int) {
	t.Helper()
	if w := apiRequest(t, testCtx, method, path, apiKey, body); w.Code != status {
		t.Fatalf("%s %s: expected %d, got %d %s", method, path, status, w.Code, w.Body.String())
	}
}

func TestScopedAPIKeyReadOnly(t *testing.T) {
	testCtx := setupTest(t)
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "Reader", Access: "read"})
	group := models.Group{Name: "G", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "a@example.com"}}}}
	expectStatus(t, testCtx, http.MethodGet, "/api/groups/", key, nil, http.StatusOK)
	expectStatus(t, testCtx, http.MethodGet, "/api/campaigns/", key, nil, http.StatusOK)
	expectStatus(t, testCtx, http.MethodPost, "/api/groups/", key, group, http.StatusForbidden)
	// The same request works with the full-access key
	expectStatus(t, testCtx, http.MethodPost, "/api/groups/", testCtx.apiKey, group, http.StatusCreated)
}

func TestScopedAPIKeyResources(t *testing.T) {
	testCtx := setupTest(t)
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "Groups", Access: "write", Resources: []string{"groups"}})
	group := models.Group{Name: "G", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "a@example.com"}}}}
	expectStatus(t, testCtx, http.MethodPost, "/api/groups/", key, group, http.StatusCreated)
	expectStatus(t, testCtx, http.MethodGet, "/api/campaigns/", key, nil, http.StatusForbidden)
	expectStatus(t, testCtx, http.MethodGet, "/api/templates/", key, nil, http.StatusForbidden)
}

func TestScopedAPIKeyCannotManageKeys(t *testing.T) {
	testCtx := setupTest(t)
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "Everything", Access: "write"})
	expectStatus(t, testCtx, http.MethodGet, "/api/api_keys/", key, nil, http.StatusForbidden)
	expectStatus(t, testCtx, http.MethodPost, "/api/api_keys/", key, apiKeyRequest{Name: "More", Access: "write"}, http.StatusForbidden)
	expectStatus(t, testCtx, http.MethodPost, "/api/reset", key, nil, http.StatusForbidden)
	u, _ := models.GetUser(1)
	if u.ApiKey != testCtx.apiKey {
		t.Fatal("expected the full-access key to be unchanged")
	}
}

func TestScopedAPIKeyLimitedByRole(t *testing.T) {
	testCtx := setupTest(t)
	user := createUnpriviledgedUser(t, models.RoleUser)
	key := createScopedKey(t, testCtx, user.ApiKey, apiKeyRequest{Name: "Everything", Access: "write"})
	expectStatus(t, testCtx, http.MethodGet, "/api/users/", key, nil, http.StatusForbidden)
	expectStatus(t, testCtx, http.MethodGet, "/api/audit_log/", key, nil, http.StatusForbidden)
	expectStatus(t, testCtx, http.MethodGet, "/api/groups/", key, nil, http.StatusOK)
}

func TestScopedAPIKeyExpiryAndDeletion(t *testing.T) {
	testCtx := setupTest(t)
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "Temporary", Access: "read", ExpiresInDays: 7})
	expectStatus(t, testCtx, http.MethodGet, "/api/groups/", key, nil, http.StatusOK)

	w := apiRequest(t, testCtx, http.MethodGet, "/api/api_keys/", testCtx.apiKey, nil)
	keys := []models.APIKey{}
	json.NewDecoder(w.Body).Decode(&keys)
	if len(keys) != 1 || keys[0].ExpiresAt == nil || keys[0].LastUsedAt == nil {
		t.Fatalf("unexpected keys: %+v", keys)
	}
	if strings.Contains(w.Body.String(), key) {
		t.Fatal("expected the key itself not to be listed")
	}
	// Other users can't delete it
	user := createUnpriviledgedUser(t, models.RoleUser)
	expectStatus(t, testCtx, http.MethodDelete, fmt.Sprintf("/api/api_keys/%d", keys[0].Id), user.ApiKey, nil, http.StatusNotFound)
	expectStatus(t, testCtx, http.MethodDelete, fmt.Sprintf("/api/api_keys/%d", keys[0].Id), testCtx.apiKey, nil, http.StatusOK)
	expectStatus(t, testCtx, http.MethodGet, "/api/groups/", key, nil, http.StatusUnauthorized)
}

func TestScopedAPIKeyValidation(t *testing.T) {
	testCtx := setupTest(t)
	expectStatus(t, testCtx, http.MethodPost, "/api/api_keys/", testCtx.apiKey, apiKeyRequest{Access: "read"}, http.StatusBadRequest)
	expectStatus(t, testCtx, http.MethodPost, "/api/api_keys/", testCtx.apiKey, apiKeyRequest{Name: "x", Access: "all"}, http.StatusBadRequest)
	expectStatus(t, testCtx, http.MethodPost, "/api/api_keys/", testCtx.apiKey, apiKeyRequest{Name: "x", Access: "read", Resources: []string{"api_keys"}}, http.StatusBadRequest)
	expectStatus(t, testCtx, http.MethodPost, "/api/api_keys/", testCtx.apiKey, apiKeyRequest{Name: "x", Access: "read", ExpiresInDays: -1}, http.StatusBadRequest)
}

func TestScopedAPIKeyAudited(t *testing.T) {
	testCtx := setupTest(t)
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "Reader", Access: "read"})
	expectStatus(t, testCtx, http.MethodDelete, "/api/groups/1", key, nil, http.StatusForbidden)
	logs, _ := models.GetAuditLogs(models.AuditLogQuery{Action: models.AuditAPIRequestDenied})
	if len(logs) != 1 || !strings.Contains(logs[0].Details, "(Reader)") || logs[0].Username != "admin" {
		t.Fatalf("unexpected audit entries: %+v", logs)
	}
}
