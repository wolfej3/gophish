package models

import (
	"time"

	"gopkg.in/check.v1"
)

// seedCampaign inserts a minimal campaign in the given team and returns it.
func (s *ModelsSuite) seedCampaign(ch *check.C, name string, teamID int64) Campaign {
	c := Campaign{Name: name, UserId: 1, TeamId: teamID, CreatedDate: time.Now().UTC(), Status: CampaignInProgress}
	ch.Assert(db.Create(&c).Error, check.Equals, nil)
	return c
}

// seedResult inserts a result for an email in a campaign.
func (s *ModelsSuite) seedResult(ch *check.C, c Campaign, email, first, last, status string, reported bool, when time.Time) {
	r := Result{
		CampaignId:    c.Id,
		UserId:        1,
		TeamId:        c.TeamId,
		RId:           email + "-" + c.Name,
		Status:        status,
		Reported:      reported,
		SendDate:      when,
		ModifiedDate:  when,
		BaseRecipient: BaseRecipient{Email: email, FirstName: first, LastName: last},
	}
	ch.Assert(db.Create(&r).Error, check.Equals, nil)
}

// seedEvent inserts an event for an email in a campaign.
func (s *ModelsSuite) seedEvent(ch *check.C, c Campaign, email, message string, when time.Time) {
	e := Event{CampaignId: c.Id, Email: email, Message: message, Time: when}
	ch.Assert(db.Create(&e).Error, check.Equals, nil)
}

func (s *ModelsSuite) TestGetPeopleAggregates(ch *check.C) {
	t0 := time.Now().UTC().Add(-48 * time.Hour)
	c1 := s.seedCampaign(ch, "Campaign One", 1)
	c2 := s.seedCampaign(ch, "Campaign Two", 1)

	// Alice: in both campaigns. Opened+clicked in c1, submitted in c2,
	// reported c2.
	s.seedResult(ch, c1, "alice@example.com", "Alice", "Adams", EventClicked, false, t0)
	s.seedResult(ch, c2, "alice@example.com", "Alice", "Adams", EventDataSubmit, true, t0.Add(time.Hour))
	s.seedEvent(ch, c1, "alice@example.com", EventOpened, t0.Add(time.Minute))
	s.seedEvent(ch, c1, "alice@example.com", EventClicked, t0.Add(2*time.Minute))
	// Clicked twice in the same campaign still counts as one campaign
	s.seedEvent(ch, c1, "alice@example.com", EventClicked, t0.Add(3*time.Minute))
	s.seedEvent(ch, c2, "alice@example.com", EventOpened, t0.Add(time.Hour))
	s.seedEvent(ch, c2, "alice@example.com", EventClicked, t0.Add(time.Hour+time.Minute))
	s.seedEvent(ch, c2, "alice@example.com", EventDataSubmit, t0.Add(time.Hour+2*time.Minute))

	// Bob: only in c1, opened only
	s.seedResult(ch, c1, "bob@example.com", "Bob", "Baker", EventOpened, false, t0)
	s.seedEvent(ch, c1, "bob@example.com", EventOpened, t0.Add(time.Minute))

	people, err := GetPeople(1)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(people), check.Equals, 2)

	byEmail := map[string]Person{}
	for _, p := range people {
		byEmail[p.Email] = p
	}
	alice := byEmail["alice@example.com"]
	ch.Assert(alice.Campaigns, check.Equals, int64(2))
	ch.Assert(alice.Opened, check.Equals, int64(2))
	ch.Assert(alice.Clicked, check.Equals, int64(2))
	ch.Assert(alice.Submitted, check.Equals, int64(1))
	ch.Assert(alice.Reported, check.Equals, int64(1))
	ch.Assert(alice.FirstName, check.Equals, "Alice")
	ch.Assert(alice.LastEvent, check.NotNil)

	bob := byEmail["bob@example.com"]
	ch.Assert(bob.Campaigns, check.Equals, int64(1))
	ch.Assert(bob.Opened, check.Equals, int64(1))
	ch.Assert(bob.Clicked, check.Equals, int64(0))
	ch.Assert(bob.Reported, check.Equals, int64(0))
}

func (s *ModelsSuite) TestPeopleScopedByTeam(ch *check.C) {
	other := Team{Name: "Other", TenantId: s.defaultTenantID(ch)}
	ch.Assert(PostTeam(&other), check.Equals, nil)
	c1 := s.seedCampaign(ch, "Mine", 1)
	c2 := s.seedCampaign(ch, "Theirs", other.Id)
	s.seedResult(ch, c1, "mine@example.com", "M", "I", EventSent, false, time.Now().UTC())
	s.seedResult(ch, c2, "theirs@example.com", "T", "H", EventSent, false, time.Now().UTC())

	people, err := GetPeople(1)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(people), check.Equals, 1)
	ch.Assert(people[0].Email, check.Equals, "mine@example.com")

	// The other team only sees its own
	otherPeople, err := GetPeople(other.Id)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(otherPeople), check.Equals, 1)
	ch.Assert(otherPeople[0].Email, check.Equals, "theirs@example.com")
}

func (s *ModelsSuite) TestGetPersonDetail(ch *check.C) {
	t0 := time.Now().UTC().Add(-24 * time.Hour)
	c1 := s.seedCampaign(ch, "First", 1)
	c2 := s.seedCampaign(ch, "Second", 1)
	s.seedResult(ch, c1, "carol@example.com", "Carol", "Clark", EventClicked, false, t0)
	s.seedResult(ch, c2, "carol@example.com", "Carol", "Clark", EventOpened, true, t0.Add(time.Hour))
	s.seedEvent(ch, c1, "carol@example.com", EventOpened, t0.Add(time.Minute))
	s.seedEvent(ch, c1, "carol@example.com", EventClicked, t0.Add(2*time.Minute))
	s.seedEvent(ch, c2, "carol@example.com", EventOpened, t0.Add(time.Hour))

	detail, err := GetPerson(1, "carol@example.com")
	ch.Assert(err, check.Equals, nil)
	ch.Assert(detail.Email, check.Equals, "carol@example.com")
	ch.Assert(detail.FirstName, check.Equals, "Carol")
	ch.Assert(detail.Campaigns, check.Equals, int64(2))
	ch.Assert(detail.Opened, check.Equals, int64(2))
	ch.Assert(detail.Clicked, check.Equals, int64(1))
	ch.Assert(detail.Reported, check.Equals, int64(1))
	ch.Assert(len(detail.History), check.Equals, 2)
	// History is newest first
	ch.Assert(detail.History[0].Name, check.Equals, "Second")
	ch.Assert(len(detail.Timeline), check.Equals, 3)
	ch.Assert(detail.Timeline[0].Time.After(detail.Timeline[2].Time), check.Equals, true)

	// Someone with no activity
	empty, err := GetPerson(1, "nobody@example.com")
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(empty.History), check.Equals, 0)
	ch.Assert(empty.Campaigns, check.Equals, int64(0))
}
