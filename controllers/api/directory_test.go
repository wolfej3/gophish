package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
)

// newGraphDirServer returns a fake Microsoft Graph serving a fixed set of
// members, so a sync has something to pull.
func newGraphDirServer(t *testing.T, members []map[string]string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"value": members})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// setupDirectoryTest builds an API server with one Entra connector pointing
// at the given mock server.
func setupDirectoryTest(t *testing.T, graphURL string) *testContext {
	testCtx := setupTest(t)
	testCtx.apiServer = NewServer(WithDirectoryConnectors([]config.DirectoryConnector{{
		Name: "corp", Provider: "msgraph", TenantID: "t", ClientID: "c", ClientSecret: "s",
		TokenURL: graphURL + "/token", BaseURL: graphURL,
	}}))
	return testCtx
}

func TestDirectoryConnectorsListed(t *testing.T) {
	testCtx := setupDirectoryTest(t, "http://example.invalid")
	w := apiRequest(t, testCtx, http.MethodGet, "/api/directory/connectors", testCtx.apiKey, nil)
	infos := []directoryConnectorInfo{}
	json.NewDecoder(w.Body).Decode(&infos)
	if len(infos) != 1 || infos[0].Name != "corp" || infos[0].Provider != "msgraph" {
		t.Fatalf("unexpected connectors: %+v", infos)
	}
}

func TestGroupSyncReplacesMembers(t *testing.T) {
	server := newGraphDirServer(t, []map[string]string{
		{"mail": "alice@example.com", "givenName": "Alice", "surname": "Adams"},
		{"mail": "bob@example.com", "givenName": "Bob", "surname": "Baker"},
	})
	testCtx := setupDirectoryTest(t, server.URL)

	// Create a group with one hand-added member
	group := models.Group{Name: "Synced", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "old@example.com"}}}}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/groups/", testCtx.apiKey, group)
	created := models.Group{}
	json.NewDecoder(w.Body).Decode(&created)

	// Attach a directory source
	w = apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/groups/%d/source", created.Id), testCtx.apiKey,
		models.DirectorySource{Connector: "corp", ExternalID: "g1"})
	if w.Code != http.StatusOK {
		t.Fatalf("setting source failed: %d %s", w.Code, w.Body.String())
	}
	// Unknown connector is rejected
	if w := apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/groups/%d/source", created.Id), testCtx.apiKey,
		models.DirectorySource{Connector: "nope", ExternalID: "g1"}); w.Code != http.StatusBadRequest {
		t.Fatalf("expected unknown connector rejected, got %d", w.Code)
	}

	// Sync
	w = apiRequest(t, testCtx, http.MethodPost, fmt.Sprintf("/api/groups/%d/sync", created.Id), testCtx.apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("sync failed: %d %s", w.Code, w.Body.String())
	}
	synced := models.Group{}
	json.NewDecoder(w.Body).Decode(&synced)
	emails := map[string]bool{}
	for _, tg := range synced.Targets {
		emails[tg.Email] = true
	}
	if len(synced.Targets) != 2 || !emails["alice@example.com"] || !emails["bob@example.com"] || emails["old@example.com"] {
		t.Fatalf("expected the directory to replace the members, got %+v", synced.Targets)
	}

	// The source records a successful sync
	w = apiRequest(t, testCtx, http.MethodGet, fmt.Sprintf("/api/groups/%d/source", created.Id), testCtx.apiKey, nil)
	source := models.DirectorySource{}
	json.NewDecoder(w.Body).Decode(&source)
	if source.LastSyncedAt == nil || source.LastError != "" {
		t.Fatalf("expected a recorded successful sync, got %+v", source)
	}
}

func TestGroupSyncWithoutSource(t *testing.T) {
	testCtx := setupDirectoryTest(t, "http://example.invalid")
	group := models.Group{Name: "Manual", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "a@example.com"}}}}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/groups/", testCtx.apiKey, group)
	created := models.Group{}
	json.NewDecoder(w.Body).Decode(&created)
	if w := apiRequest(t, testCtx, http.MethodPost, fmt.Sprintf("/api/groups/%d/sync", created.Id), testCtx.apiKey, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("expected sync without a source to be rejected, got %d", w.Code)
	}
}

func TestGroupSyncEmptyDirectoryKeepsMembers(t *testing.T) {
	// An empty directory response must not wipe the group.
	server := newGraphDirServer(t, []map[string]string{})
	testCtx := setupDirectoryTest(t, server.URL)
	group := models.Group{Name: "Keep", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "keep@example.com"}}}}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/groups/", testCtx.apiKey, group)
	created := models.Group{}
	json.NewDecoder(w.Body).Decode(&created)
	apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/groups/%d/source", created.Id), testCtx.apiKey, models.DirectorySource{Connector: "corp", ExternalID: "g1"})
	if w := apiRequest(t, testCtx, http.MethodPost, fmt.Sprintf("/api/groups/%d/sync", created.Id), testCtx.apiKey, nil); w.Code == http.StatusOK {
		t.Fatalf("expected an empty directory to fail the sync, got %d", w.Code)
	}
	// The original member is still there
	got, _ := models.GetGroup(created.Id, 1)
	if len(got.Targets) != 1 || got.Targets[0].Email != "keep@example.com" {
		t.Fatalf("expected the hand-added member to survive, got %+v", got.Targets)
	}
	// And the failure was recorded
	source, _ := models.GetDirectorySource(created.Id)
	if source.LastError == "" {
		t.Fatal("expected the sync error to be recorded")
	}
}
