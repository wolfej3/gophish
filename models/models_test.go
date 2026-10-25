package models

import (
	"testing"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/internal/testdb"
	"gopkg.in/check.v1"
	"gorm.io/gorm"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

type ModelsSuite struct {
	config *config.Config
}

var _ = check.Suite(&ModelsSuite{})

func (s *ModelsSuite) SetUpSuite(c *check.C) {
	conf := testdb.Config(c)
	s.config = conf
	err := Setup(conf)
	if err != nil {
		c.Fatalf("Failed creating database: %v", err)
	}
}

func (s *ModelsSuite) TearDownTest(c *check.C) {
	// Clear database tables between each test. If new tables are
	// used in this test suite they will need to be cleaned up here.
	cleanup := db.Session(&gorm.Session{AllowGlobalUpdate: true})
	for _, model := range []interface{}{
		&Group{}, &Target{}, &GroupTarget{}, &SMTP{}, &Header{}, &Page{}, &Template{}, &Attachment{},
		&Result{}, &Event{}, &MailLog{}, &Campaign{}, &Session{},
		&CampaignSchedule{}, &ScheduleGroup{},
		&DirectorySource{},
		&Team{}, &TeamMember{}, &Tenant{},
	} {
		if err := cleanup.Delete(model).Error; err != nil {
			panic(err)
		}
	}

	// Reset users table to default state.
	if err := db.Where("id <> ?", 1).Delete(&User{}).Error; err != nil {
		panic(err)
	}
	if err := db.Model(&User{}).Where("id = ?", 1).Update("username", "admin").Error; err != nil {
		panic(err)
	}
	// Recreate the default tenant and team with the admin as an editor, as a
	// fresh install would have, and keep the admin pinned to the default tenant.
	team, err := GetDefaultTeam()
	if err != nil {
		panic(err)
	}
	if err := db.Model(&User{}).Where("id = ?", 1).Updates(map[string]interface{}{"tenant_id": team.TenantId, "default_team_id": 0}).Error; err != nil {
		panic(err)
	}
	if err := SetTeamMember(team.Id, 1, TeamRoleEditor); err != nil {
		panic(err)
	}
}

func (s *ModelsSuite) createCampaignDependencies(ch *check.C, optional ...string) Campaign {
	// we use the optional parameter to pass an alternative subject
	group := Group{Name: "Test Group"}
	group.Targets = []Target{
		Target{BaseRecipient: BaseRecipient{Email: "test1@example.com", FirstName: "First", LastName: "Example"}},
		Target{BaseRecipient: BaseRecipient{Email: "test2@example.com", FirstName: "Second", LastName: "Example"}},
		Target{BaseRecipient: BaseRecipient{Email: "test3@example.com", FirstName: "Second", LastName: "Example"}},
		Target{BaseRecipient: BaseRecipient{Email: "test4@example.com", FirstName: "Second", LastName: "Example"}},
	}
	group.UserId = 1
	group.TeamId = 1
	ch.Assert(PostGroup(&group), check.Equals, nil)

	// Add a template
	t := Template{Name: "Test Template"}
	if len(optional) > 0 {
		t.Subject = optional[0]
	} else {
		t.Subject = "{{.RId}} - Subject"
	}
	t.Text = "{{.RId}} - Text"
	t.HTML = "{{.RId}} - HTML"
	t.UserId = 1
	t.TeamId = 1
	ch.Assert(PostTemplate(&t), check.Equals, nil)

	// Add a landing page
	p := Page{Name: "Test Page"}
	p.HTML = "<html>Test</html>"
	p.UserId = 1
	p.TeamId = 1
	ch.Assert(PostPage(&p), check.Equals, nil)

	// Add a sending profile
	smtp := SMTP{Name: "Test Page"}
	smtp.UserId = 1
	smtp.TeamId = 1
	smtp.Host = "example.com"
	smtp.FromAddress = "test@test.com"
	ch.Assert(PostSMTP(&smtp), check.Equals, nil)

	c := Campaign{Name: "Test campaign"}
	c.UserId = 1
	c.TeamId = 1
	c.Template = t
	c.Page = p
	c.SMTP = smtp
	c.Groups = []Group{group}
	return c
}

func (s *ModelsSuite) createCampaign(ch *check.C) Campaign {
	c := s.createCampaignDependencies(ch)
	// Setup and "launch" our campaign
	ch.Assert(PostCampaign(&c, c.UserId, 1), check.Equals, nil)

	// For comparing the dates, we need to fetch the campaign again. This is
	// to solve an issue where the campaign object right now has time down to
	// the microsecond, while in MySQL it's rounded down to the second.
	c, _ = GetCampaign(c.Id, c.UserId)
	return c
}

func setupBenchmark(b *testing.B) {
	conf := testdb.Config(b)
	err := Setup(conf)
	if err != nil {
		b.Fatalf("Failed creating database: %v", err)
	}
}

func tearDownBenchmark(b *testing.B) {
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatalf("error getting database handle: %v", err)
	}
	err = sqlDB.Close()
	if err != nil {
		b.Fatalf("error closing database: %v", err)
	}
}

func resetBenchmark(b *testing.B) {
	cleanup := db.Session(&gorm.Session{AllowGlobalUpdate: true})
	for _, model := range []interface{}{
		&Group{}, &Target{}, &GroupTarget{}, &SMTP{}, &Header{}, &Page{}, &Template{}, &Attachment{},
		&Result{}, &Event{}, &MailLog{}, &Campaign{}, &Session{},
		&CampaignSchedule{}, &ScheduleGroup{},
		&DirectorySource{},
		&Team{}, &TeamMember{}, &Tenant{},
	} {
		if err := cleanup.Delete(model).Error; err != nil {
			panic(err)
		}
	}

	// Reset users table to default state.
	if err := db.Where("id <> ?", 1).Delete(&User{}).Error; err != nil {
		panic(err)
	}
	if err := db.Model(&User{}).Where("id = ?", 1).Update("username", "admin").Error; err != nil {
		panic(err)
	}
	// Recreate the default tenant and team with the admin as an editor, as a
	// fresh install would have, and keep the admin pinned to the default tenant.
	team, err := GetDefaultTeam()
	if err != nil {
		panic(err)
	}
	if err := db.Model(&User{}).Where("id = ?", 1).Updates(map[string]interface{}{"tenant_id": team.TenantId, "default_team_id": 0}).Error; err != nil {
		panic(err)
	}
	if err := SetTeamMember(team.Id, 1, TeamRoleEditor); err != nil {
		panic(err)
	}
}
