package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

// TestCrossTeamObjectAccessByID is the core check for CVE-2026-107270: a user
// in one team must not be able to GET, PUT, or DELETE another team's object
// by ID. Each attempt must 404 and leave the object intact.
func TestCrossTeamObjectAccessByID(t *testing.T) {
	testCtx := setupTest(t)
	alice, aliceTeam := userInTeam(t, "alice", models.TeamRoleEditor)
	carol, _ := userInTeam(t, "carol", models.TeamRoleEditor)

	targets := []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}

	cases := []struct {
		name     string
		resource string
		create   func() int64
		exists   func(id int64) bool
		putBody  func(id int64) interface{}
	}{
		{
			name:     "groups",
			resource: "groups",
			create: func() int64 {
				g := models.Group{Name: "A-Group", TeamId: aliceTeam.Id, UserId: alice.Id, Targets: targets}
				if err := models.PostGroup(&g); err != nil {
					t.Fatalf("create group: %v", err)
				}
				return g.Id
			},
			exists:  func(id int64) bool { _, err := models.GetGroup(id, aliceTeam.Id); return err == nil },
			putBody: func(id int64) interface{} { return models.Group{Id: id, Name: "Hacked", Targets: targets} },
		},
		{
			name:     "templates",
			resource: "templates",
			create: func() int64 {
				tm := models.Template{Name: "A-Template", Text: "hi", TeamId: aliceTeam.Id, UserId: alice.Id}
				if err := models.PostTemplate(&tm); err != nil {
					t.Fatalf("create template: %v", err)
				}
				return tm.Id
			},
			exists:  func(id int64) bool { _, err := models.GetTemplate(id, aliceTeam.Id); return err == nil },
			putBody: func(id int64) interface{} { return models.Template{Id: id, Name: "Hacked", Text: "x"} },
		},
		{
			name:     "pages",
			resource: "pages",
			create: func() int64 {
				p := models.Page{Name: "A-Page", HTML: "<html></html>", TeamId: aliceTeam.Id, UserId: alice.Id}
				if err := models.PostPage(&p); err != nil {
					t.Fatalf("create page: %v", err)
				}
				return p.Id
			},
			exists:  func(id int64) bool { _, err := models.GetPage(id, aliceTeam.Id); return err == nil },
			putBody: func(id int64) interface{} { return models.Page{Id: id, Name: "Hacked", HTML: "<html></html>"} },
		},
		{
			name:     "smtp",
			resource: "smtp",
			create: func() int64 {
				s := models.SMTP{Name: "A-SMTP", FromAddress: "a@b.com", Host: "smtp.example.com:25", Interface: models.SMTPInterface, TeamId: aliceTeam.Id, UserId: alice.Id}
				if err := models.PostSMTP(&s); err != nil {
					t.Fatalf("create smtp: %v", err)
				}
				return s.Id
			},
			exists: func(id int64) bool { _, err := models.GetSMTP(id, aliceTeam.Id); return err == nil },
			putBody: func(id int64) interface{} {
				return models.SMTP{Id: id, Name: "Hacked", FromAddress: "a@b.com", Host: "smtp.example.com:25", Interface: models.SMTPInterface}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.create()
			base := fmt.Sprintf("/api/%s/%d", tc.resource, id)
			// Carol (another team) must be refused on every verb.
			for _, m := range []struct {
				method string
				body   interface{}
			}{
				{http.MethodGet, nil},
				{http.MethodPut, tc.putBody(id)},
				{http.MethodDelete, nil},
			} {
				w := apiRequest(t, testCtx, m.method, base, carol.ApiKey, m.body)
				if w.Code != http.StatusNotFound {
					t.Fatalf("%s %s by another team: expected 404, got %d %s", m.method, base, w.Code, w.Body.String())
				}
			}
			// The object must still exist and be unchanged.
			if !tc.exists(id) {
				t.Fatalf("%s %d was affected by another team's requests", tc.resource, id)
			}
		})
	}
}

// TestViewerCannotWriteAnyResource confirms a viewer on the owning team is
// refused writes across resources.
func TestViewerCannotWriteAnyResource(t *testing.T) {
	testCtx := setupTest(t)
	viewer, team := userInTeam(t, "viewer", models.TeamRoleViewer)
	_ = viewer
	admin, _ := models.GetUser(1)
	targets := []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}
	g := models.Group{Name: "Seeded", TeamId: team.Id, UserId: admin.Id, Targets: targets}
	models.PostGroup(&g)

	for _, res := range []string{"groups", "templates", "pages", "smtp"} {
		w := apiRequest(t, testCtx, http.MethodPost, "/api/"+res+"/", viewer.ApiKey, map[string]string{"name": "x"})
		if w.Code != http.StatusForbidden {
			t.Fatalf("viewer POST /api/%s/ expected 403, got %d", res, w.Code)
		}
	}
}

// TestPutPreservesCreator confirms a PUT by a different team member keeps the
// original creator (UserId), matching the README's "records which user
// created it".
func TestPutPreservesCreator(t *testing.T) {
	testCtx := setupTest(t)
	alice, team := userInTeam(t, "alice", models.TeamRoleEditor)
	// Bob is a second editor on the same team.
	role, _ := models.GetRoleBySlug(models.RoleUser)
	bob := &models.User{Username: "bob", Hash: "x", ApiKey: "bob-key", Role: role, RoleID: role.ID}
	models.PutUser(bob)
	models.SetTeamMember(team.Id, bob.Id, models.TeamRoleEditor)

	g := models.Group{Name: "Owned", TeamId: team.Id, UserId: alice.Id,
		Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com"}}}}
	if err := models.PostGroup(&g); err != nil {
		t.Fatalf("create group: %v", err)
	}
	// Bob edits it.
	body := models.Group{Id: g.Id, Name: "Owned v2", Targets: []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t2@example.com"}}}}
	w := apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/groups/%d", g.Id), bob.ApiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("bob's edit failed: %d %s", w.Code, w.Body.String())
	}
	got, _ := models.GetGroup(g.Id, team.Id)
	if got.UserId != alice.Id {
		t.Fatalf("expected creator to stay alice (%d), got %d", alice.Id, got.UserId)
	}
}
