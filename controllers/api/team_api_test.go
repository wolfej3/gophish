package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

func TestTeamCRUD(t *testing.T) {
	testCtx := setupTest(t)
	// Create
	w := apiRequest(t, testCtx, http.MethodPost, "/api/teams/", testCtx.apiKey, models.Team{Name: "Red"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected create, got %d %s", w.Code, w.Body.String())
	}
	team := models.Team{}
	json.NewDecoder(w.Body).Decode(&team)
	// Duplicate name
	if w := apiRequest(t, testCtx, http.MethodPost, "/api/teams/", testCtx.apiKey, models.Team{Name: "Red"}); w.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", w.Code)
	}
	// Rename
	if w := apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/teams/%d", team.Id), testCtx.apiKey, models.Team{Name: "Crimson"}); w.Code != http.StatusOK {
		t.Fatalf("expected rename, got %d", w.Code)
	}
	// Delete
	if w := apiRequest(t, testCtx, http.MethodDelete, fmt.Sprintf("/api/teams/%d", team.Id), testCtx.apiKey, nil); w.Code != http.StatusOK {
		t.Fatalf("expected delete, got %d %s", w.Code, w.Body.String())
	}
}

func TestTeamManagementAdminOnly(t *testing.T) {
	testCtx := setupTest(t)
	user := createUnpriviledgedUser(t, models.RoleUser)
	// A regular user can list teams (for the switcher)...
	if w := apiRequest(t, testCtx, http.MethodGet, "/api/teams/", user.ApiKey, nil); w.Code != http.StatusOK {
		t.Fatalf("expected a user to list teams, got %d", w.Code)
	}
	// ...but can't create, rename, or delete them
	if w := apiRequest(t, testCtx, http.MethodPost, "/api/teams/", user.ApiKey, models.Team{Name: "Nope"}); w.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden create, got %d", w.Code)
	}
}

func TestTeamMembers(t *testing.T) {
	testCtx := setupTest(t)
	w := apiRequest(t, testCtx, http.MethodPost, "/api/teams/", testCtx.apiKey, models.Team{Name: "Blue"})
	team := models.Team{}
	json.NewDecoder(w.Body).Decode(&team)
	user := createUnpriviledgedUser(t, models.RoleUser)

	// Add as viewer
	if w := apiRequest(t, testCtx, http.MethodPost, fmt.Sprintf("/api/teams/%d/members", team.Id), testCtx.apiKey, teamMemberRequest{UserId: user.Id, Role: "viewer"}); w.Code != http.StatusOK {
		t.Fatalf("expected member added, got %d %s", w.Code, w.Body.String())
	}
	// Bad role
	if w := apiRequest(t, testCtx, http.MethodPost, fmt.Sprintf("/api/teams/%d/members", team.Id), testCtx.apiKey, teamMemberRequest{UserId: user.Id, Role: "boss"}); w.Code != http.StatusBadRequest {
		t.Fatalf("expected bad role rejected, got %d", w.Code)
	}
	// List
	w = apiRequest(t, testCtx, http.MethodGet, fmt.Sprintf("/api/teams/%d/members", team.Id), testCtx.apiKey, nil)
	members := []models.TeamMemberDetail{}
	json.NewDecoder(w.Body).Decode(&members)
	if len(members) != 1 || members[0].Role != "viewer" || members[0].Username != user.Username {
		t.Fatalf("unexpected members: %+v", members)
	}
	// Remove
	if w := apiRequest(t, testCtx, http.MethodDelete, fmt.Sprintf("/api/teams/%d/members/%d", team.Id, user.Id), testCtx.apiKey, nil); w.Code != http.StatusOK {
		t.Fatalf("expected member removed, got %d", w.Code)
	}
	w = apiRequest(t, testCtx, http.MethodGet, fmt.Sprintf("/api/teams/%d/members", team.Id), testCtx.apiKey, nil)
	members = []models.TeamMemberDetail{}
	json.NewDecoder(w.Body).Decode(&members)
	if len(members) != 0 {
		t.Fatalf("expected no members, got %+v", members)
	}
}

func TestTeamScopedKeyCannotManage(t *testing.T) {
	testCtx := setupTest(t)
	key := createScopedKey(t, testCtx, testCtx.apiKey, apiKeyRequest{Name: "k", Access: "write"})
	if w := apiRequest(t, testCtx, http.MethodGet, "/api/teams/", key, nil); w.Code != http.StatusForbidden {
		t.Fatalf("expected a scoped key to be refused, got %d", w.Code)
	}
}
