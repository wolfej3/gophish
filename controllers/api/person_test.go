package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gophish/gophish/models"
)

// seedPersonData creates a campaign in team 1 with a result and some events
// for the given email.
func seedPersonData(t *testing.T, email string, events []string) {
	t.Helper()
	c := models.Campaign{Name: "C-" + email + "-" + events[0], UserId: 1, TeamId: 1, CreatedDate: time.Now().UTC(), Status: models.CampaignInProgress}
	if err := models.InsertCampaign(&c); err != nil {
		t.Fatalf("error seeding campaign: %v", err)
	}
	r := models.Result{CampaignId: c.Id, UserId: 1, TeamId: 1, RId: email + c.Name, Status: events[len(events)-1],
		SendDate: time.Now().UTC(), ModifiedDate: time.Now().UTC(), BaseRecipient: models.BaseRecipient{Email: email, FirstName: "Test", LastName: "User"}}
	if err := models.InsertResult(&r); err != nil {
		t.Fatalf("error seeding result: %v", err)
	}
	for i, ev := range events {
		if err := models.InsertEvent(&models.Event{CampaignId: c.Id, Email: email, Message: ev, Time: time.Now().UTC().Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatalf("error seeding event: %v", err)
		}
	}
}

func TestPeopleEndpoint(t *testing.T) {
	testCtx := setupTest(t)
	seedPersonData(t, "alice@example.com", []string{models.EventSent, models.EventOpened, models.EventClicked})

	w := apiRequest(t, testCtx, http.MethodGet, "/api/people/", testCtx.apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	people := []models.Person{}
	json.NewDecoder(w.Body).Decode(&people)
	if len(people) != 1 || people[0].Email != "alice@example.com" {
		t.Fatalf("unexpected people: %+v", people)
	}
	if people[0].Opened != 1 || people[0].Clicked != 1 {
		t.Fatalf("unexpected aggregates: %+v", people[0])
	}
}

func TestPersonEndpoint(t *testing.T) {
	testCtx := setupTest(t)
	seedPersonData(t, "bob@example.com", []string{models.EventSent, models.EventOpened})

	w := apiRequest(t, testCtx, http.MethodGet, "/api/people/"+url.PathEscape("bob@example.com"), testCtx.apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
	}
	detail := models.PersonDetail{}
	json.NewDecoder(w.Body).Decode(&detail)
	if detail.Email != "bob@example.com" || len(detail.History) != 1 || detail.Opened != 1 {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	// Unknown person is a 404
	if w := apiRequest(t, testCtx, http.MethodGet, "/api/people/"+url.PathEscape("nobody@example.com"), testCtx.apiKey, nil); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown person, got %d", w.Code)
	}
}

func TestPeopleScopedToTeam(t *testing.T) {
	testCtx := setupTest(t)
	// A person in another team isn't visible from the default team
	other := models.Team{Name: "Other"}
	models.PostTeam(&other)
	c := models.Campaign{Name: "Theirs", UserId: 1, TeamId: other.Id, CreatedDate: time.Now().UTC(), Status: models.CampaignInProgress}
	models.InsertCampaign(&c)
	models.InsertResult(&models.Result{CampaignId: c.Id, UserId: 1, TeamId: other.Id, RId: "x", Status: models.EventSent,
		SendDate: time.Now().UTC(), ModifiedDate: time.Now().UTC(), BaseRecipient: models.BaseRecipient{Email: "them@example.com"}})

	w := apiRequest(t, testCtx, http.MethodGet, "/api/people/", testCtx.apiKey, nil)
	people := []models.Person{}
	json.NewDecoder(w.Body).Decode(&people)
	if len(people) != 0 {
		t.Fatalf("expected no people in the default team, got %+v", people)
	}
}
