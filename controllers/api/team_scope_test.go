package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gophish/gophish/models"
)

// userInTeam creates a user with the given role on a new team, and returns
// the user and the team.
func userInTeam(t *testing.T, username, role string) (*models.User, models.Team) {
	t.Helper()
	userRole, err := models.GetRoleBySlug(models.RoleUser)
	if err != nil {
		t.Fatalf("error getting role: %v", err)
	}
	u := &models.User{Username: username, Hash: "x", ApiKey: username + "-key", Role: userRole, RoleID: userRole.ID}
	if err := models.PutUser(u); err != nil {
		t.Fatalf("error creating user: %v", err)
	}
	team := models.Team{Name: username + "-team"}
	if err := models.PostTeam(&team); err != nil {
		t.Fatalf("error creating team: %v", err)
	}
	if err := models.SetTeamMember(team.Id, u.Id, role); err != nil {
		t.Fatalf("error adding member: %v", err)
	}
	return u, team
}

func TestTeamObjectsSharedWithinTeam(t *testing.T) {
	testCtx := setupTest(t)
	alice, team := userInTeam(t, "alice", models.TeamRoleEditor)
	bob := &models.User{Username: "bob", Hash: "x", ApiKey: "bob-key"}
	role, _ := models.GetRoleBySlug(models.RoleUser)
	bob.Role, bob.RoleID = role, role.ID
	models.PutUser(bob)
	models.SetTeamMember(team.Id, bob.Id, models.TeamRoleEditor)

	// Alice creates a group in the shared team
	group := models.Group{Name: "Shared", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/groups/", alice.ApiKey, group)
	if w.Code != http.StatusCreated {
		t.Fatalf("alice couldn't create a group: %d %s", w.Code, w.Body.String())
	}

	// Bob, on the same team, sees it without the team header (his first team)
	w = apiRequest(t, testCtx, http.MethodGet, "/api/groups/", bob.ApiKey, nil)
	groups := []models.Group{}
	json.NewDecoder(w.Body).Decode(&groups)
	if len(groups) != 1 || groups[0].Name != "Shared" {
		t.Fatalf("bob didn't see the shared group: %+v", groups)
	}
	// The group records alice as creator (UserId isn't serialized, so check the model)
	stored, err := models.GetGroup(groups[0].Id, team.Id)
	if err != nil {
		t.Fatalf("error loading the group: %v", err)
	}
	if stored.UserId != alice.Id {
		t.Fatalf("expected the group to record alice as creator, got user %d", stored.UserId)
	}
}

func TestTeamObjectsIsolatedBetweenTeams(t *testing.T) {
	testCtx := setupTest(t)
	alice, _ := userInTeam(t, "alice", models.TeamRoleEditor)
	carol, _ := userInTeam(t, "carol", models.TeamRoleEditor)

	group := models.Group{Name: "Alice Only", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}}
	if w := apiRequest(t, testCtx, http.MethodPost, "/api/groups/", alice.ApiKey, group); w.Code != http.StatusCreated {
		t.Fatalf("alice couldn't create a group: %d", w.Code)
	}
	// Carol, on a different team, sees nothing
	w := apiRequest(t, testCtx, http.MethodGet, "/api/groups/", carol.ApiKey, nil)
	groups := []models.Group{}
	json.NewDecoder(w.Body).Decode(&groups)
	if len(groups) != 0 {
		t.Fatalf("carol saw another team's groups: %+v", groups)
	}
}

func TestTeamViewerCannotWrite(t *testing.T) {
	testCtx := setupTest(t)
	viewer, team := userInTeam(t, "viewer", models.TeamRoleViewer)
	// Seed a group in the team as the admin
	admin, _ := models.GetUser(1)
	g := models.Group{Name: "Seeded", TeamId: team.Id, UserId: admin.Id, Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}}
	models.PostGroup(&g)

	// The viewer can read the team's groups with the team header
	req := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		r := httptest.NewRequest(method, path, &buf)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+viewer.ApiKey)
		w := httptest.NewRecorder()
		testCtx.apiServer.ServeHTTP(w, r)
		return w
	}
	if w := req(http.MethodGet, "/api/groups/", nil); w.Code != http.StatusOK {
		t.Fatalf("viewer couldn't read groups: %d", w.Code)
	}
	// But can't create one
	group := models.Group{Name: "Nope", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}}
	if w := req(http.MethodPost, "/api/groups/", group); w.Code != http.StatusForbidden {
		t.Fatalf("expected viewer write to be forbidden, got %d", w.Code)
	}
}

func TestTeamHeaderSelectsTeam(t *testing.T) {
	testCtx := setupTest(t)
	// Admin is on every team; create two teams with a group each
	admin, _ := models.GetUser(1)
	teamA := models.Team{Name: "A"}
	teamB := models.Team{Name: "B"}
	models.PostTeam(&teamA)
	models.PostTeam(&teamB)
	for _, tc := range []struct {
		team models.Team
		name string
	}{{teamA, "In A"}, {teamB, "In B"}} {
		g := models.Group{Name: tc.name, TeamId: tc.team.Id, UserId: admin.Id, Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}}
		models.PostGroup(&g)
	}
	get := func(teamID string) []models.Group {
		r := httptest.NewRequest(http.MethodGet, "/api/groups/", nil)
		r.Header.Set("Authorization", "Bearer "+testCtx.apiKey)
		if teamID != "" {
			r.Header.Set("X-Gophish-Team-Id", teamID)
		}
		w := httptest.NewRecorder()
		testCtx.apiServer.ServeHTTP(w, r)
		groups := []models.Group{}
		json.NewDecoder(w.Body).Decode(&groups)
		return groups
	}
	a := get(itoa(teamA.Id))
	if len(a) != 1 || a[0].Name != "In A" {
		t.Fatalf("expected team A's group, got %+v", a)
	}
	b := get(itoa(teamB.Id))
	if len(b) != 1 || b[0].Name != "In B" {
		t.Fatalf("expected team B's group, got %+v", b)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
