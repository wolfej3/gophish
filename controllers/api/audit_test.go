package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophish/gophish/models"
)

func apiRequest(t *testing.T, testCtx *testContext, method, path, apiKey string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	w := httptest.NewRecorder()
	testCtx.apiServer.ServeHTTP(w, r)
	return w
}

func TestAuditAPIChanges(t *testing.T) {
	testCtx := setupTest(t)
	group := models.Group{Name: "Audited", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "a@example.com"}}}}
	if w := apiRequest(t, testCtx, http.MethodPost, "/api/groups/", testCtx.apiKey, group); w.Code != http.StatusCreated {
		t.Fatalf("unexpected status creating group: %d", w.Code)
	}
	// Reads aren't recorded
	apiRequest(t, testCtx, http.MethodGet, "/api/groups/", testCtx.apiKey, nil)
	// Failed and denied changes are
	apiRequest(t, testCtx, http.MethodPost, "/api/groups/", testCtx.apiKey, models.Group{})
	user := createUnpriviledgedUser(t, models.RoleUser)
	apiRequest(t, testCtx, http.MethodPost, "/api/users/", user.ApiKey, userRequest{Username: "x", Password: "longpassword", Role: "admin"})

	logs, err := models.GetAuditLogs(models.AuditLogQuery{})
	if err != nil {
		t.Fatalf("error getting audit log: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(logs), logs)
	}
	expected := []struct{ username, action, target string }{
		{"foo", models.AuditAPIRequestDenied, "POST /api/users/"},
		{"admin", models.AuditAPIRequestFailed, "POST /api/groups/"},
		{"admin", models.AuditAPIRequest, "POST /api/groups/"},
	}
	for i, e := range expected {
		if logs[i].Username != e.username || logs[i].Action != e.action || logs[i].Target != e.target {
			t.Fatalf("unexpected entry %d: %+v", i, logs[i])
		}
	}
}

func TestAuditLogAdminOnly(t *testing.T) {
	testCtx := setupTest(t)
	user := createUnpriviledgedUser(t, models.RoleUser)
	if w := apiRequest(t, testCtx, http.MethodGet, "/api/audit_log/", user.ApiKey, nil); w.Code != http.StatusForbidden {
		t.Fatalf("expected non-admins to be refused, got %d", w.Code)
	}
	models.PutAuditLog(&models.AuditLog{Username: "admin", Action: models.AuditLogin})
	w := apiRequest(t, testCtx, http.MethodGet, "/api/audit_log/?action=login", testCtx.apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	logs := []models.AuditLog{}
	json.NewDecoder(w.Body).Decode(&logs)
	if len(logs) != 1 || logs[0].Action != models.AuditLogin {
		t.Fatalf("unexpected entries: %+v", logs)
	}
}
