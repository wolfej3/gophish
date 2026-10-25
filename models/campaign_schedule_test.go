package models

import (
	"time"

	check "gopkg.in/check.v1"
)

// newSchedule builds a valid CampaignSchedule referencing the dependencies
// created by createCampaignDependencies (template/page/smtp/group), without
// persisting it.
func (s *ModelsSuite) newSchedule(c *check.C) CampaignSchedule {
	campaign := s.createCampaignDependencies(c)
	return CampaignSchedule{
		Name:           "Quarterly Awareness",
		TemplateName:   campaign.Template.Name,
		PageName:       campaign.Page.Name,
		SMTPName:       campaign.SMTP.Name,
		URL:            "http://localhost.localdomain",
		RepeatInterval: IntervalWeekly,
		Enabled:        true,
		Groups:         []ScheduleGroup{{GroupName: campaign.Groups[0].Name}},
	}
}

func (s *ModelsSuite) TestScheduleValidation(c *check.C) {
	valid := s.newSchedule(c)
	c.Assert(valid.Validate(), check.Equals, nil)

	missingName := valid
	missingName.Name = ""
	c.Assert(missingName.Validate(), check.Equals, ErrScheduleNameNotSpecified)

	missingGroups := valid
	missingGroups.Groups = nil
	c.Assert(missingGroups.Validate(), check.Equals, ErrGroupNotSpecified)

	missingTemplate := valid
	missingTemplate.TemplateName = ""
	c.Assert(missingTemplate.Validate(), check.Equals, ErrTemplateNotSpecified)

	missingPage := valid
	missingPage.PageName = ""
	c.Assert(missingPage.Validate(), check.Equals, ErrPageNotSpecified)

	missingSMTP := valid
	missingSMTP.SMTPName = ""
	c.Assert(missingSMTP.Validate(), check.Equals, ErrSMTPNotSpecified)

	badInterval := valid
	badInterval.RepeatInterval = "yearly"
	c.Assert(badInterval.Validate(), check.Equals, ErrInvalidInterval)
}

func (s *ModelsSuite) TestScheduleCRUD(c *check.C) {
	sched := s.newSchedule(c)
	c.Assert(PostCampaignSchedule(&sched, 1, 1), check.Equals, nil)
	c.Assert(sched.Id, check.Not(check.Equals), int64(0))
	// A zero next-launch date defaults to "now" (created date).
	c.Assert(sched.NextLaunchDate.IsZero(), check.Equals, false)

	got, err := GetCampaignSchedule(sched.Id, 1)
	c.Assert(err, check.Equals, nil)
	c.Assert(got.Name, check.Equals, "Quarterly Awareness")
	c.Assert(len(got.Groups), check.Equals, 1)

	// A schedule belonging to another team isn't visible.
	_, err = GetCampaignSchedule(sched.Id, 2)
	c.Assert(err, check.NotNil)

	all, err := GetCampaignSchedules(1)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(all), check.Equals, 1)

	// Update replaces the group list rather than appending.
	got.Groups = []ScheduleGroup{{GroupName: "Test Group"}}
	got.RepeatInterval = IntervalMonthly
	c.Assert(PutCampaignSchedule(&got, 1), check.Equals, nil)
	reloaded, err := GetCampaignSchedule(sched.Id, 1)
	c.Assert(err, check.Equals, nil)
	c.Assert(reloaded.RepeatInterval, check.Equals, IntervalMonthly)
	c.Assert(len(reloaded.Groups), check.Equals, 1)

	// Deletion is team-scoped and removes the group associations.
	c.Assert(DeleteCampaignSchedule(sched.Id, 2), check.Equals, ErrScheduleNotFound)
	c.Assert(DeleteCampaignSchedule(sched.Id, 1), check.Equals, nil)
	_, err = GetCampaignSchedule(sched.Id, 1)
	c.Assert(err, check.NotNil)
	groups := []ScheduleGroup{}
	c.Assert(db.Where("schedule_id=?", sched.Id).Find(&groups).Error, check.Equals, nil)
	c.Assert(len(groups), check.Equals, 0)
}

func (s *ModelsSuite) TestScheduleAdvanceInterval(c *check.C) {
	base := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)

	daily := CampaignSchedule{RepeatInterval: IntervalDaily}
	c.Assert(daily.advanceFrom(base), check.Equals, base.AddDate(0, 0, 1))

	weekly := CampaignSchedule{RepeatInterval: IntervalWeekly}
	c.Assert(weekly.advanceFrom(base), check.Equals, base.AddDate(0, 0, 7))

	monthly := CampaignSchedule{RepeatInterval: IntervalMonthly}
	c.Assert(monthly.advanceFrom(base), check.Equals, base.AddDate(0, 1, 0))

	// A one-off schedule does not repeat.
	once := CampaignSchedule{RepeatInterval: IntervalOnce}
	c.Assert(once.advanceFrom(base).IsZero(), check.Equals, true)
}

func (s *ModelsSuite) TestAdvanceScheduleWeekly(c *check.C) {
	sched := s.newSchedule(c)
	sched.NextLaunchDate = time.Now().UTC().Truncate(time.Second)
	c.Assert(PostCampaignSchedule(&sched, 1, 1), check.Equals, nil)

	runTime := sched.NextLaunchDate
	c.Assert(sched.AdvanceSchedule(runTime), check.Equals, nil)

	reloaded, err := GetCampaignSchedule(sched.Id, 1)
	c.Assert(err, check.Equals, nil)
	c.Assert(reloaded.Enabled, check.Equals, true)
	c.Assert(reloaded.NextLaunchDate.Unix(), check.Equals, runTime.AddDate(0, 0, 7).Unix())
	c.Assert(reloaded.LastLaunchDate.Unix(), check.Equals, runTime.Unix())
}

func (s *ModelsSuite) TestAdvanceScheduleOnceDisables(c *check.C) {
	sched := s.newSchedule(c)
	sched.RepeatInterval = IntervalOnce
	sched.NextLaunchDate = time.Now().UTC().Truncate(time.Second)
	c.Assert(PostCampaignSchedule(&sched, 1, 1), check.Equals, nil)

	c.Assert(sched.AdvanceSchedule(sched.NextLaunchDate), check.Equals, nil)

	reloaded, err := GetCampaignSchedule(sched.Id, 1)
	c.Assert(err, check.Equals, nil)
	c.Assert(reloaded.Enabled, check.Equals, false)
	c.Assert(reloaded.NextLaunchDate.IsZero(), check.Equals, true)
}

func (s *ModelsSuite) TestGetDueSchedules(c *check.C) {
	now := time.Now().UTC()

	due := s.newSchedule(c)
	due.Name = "Due"
	due.NextLaunchDate = now.Add(-1 * time.Minute)
	c.Assert(PostCampaignSchedule(&due, 1, 1), check.Equals, nil)

	future := s.newSchedule(c)
	future.Name = "Future"
	future.NextLaunchDate = now.Add(1 * time.Hour)
	c.Assert(PostCampaignSchedule(&future, 1, 1), check.Equals, nil)

	disabled := s.newSchedule(c)
	disabled.Name = "Disabled"
	disabled.Enabled = false
	disabled.NextLaunchDate = now.Add(-1 * time.Minute)
	c.Assert(PostCampaignSchedule(&disabled, 1, 1), check.Equals, nil)
	// PostCampaignSchedule persists Enabled as given (false here).

	ss, err := GetDueSchedules(now)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(ss), check.Equals, 1)
	c.Assert(ss[0].Name, check.Equals, "Due")
	// Groups are preloaded so the worker can build a campaign.
	c.Assert(len(ss[0].Groups), check.Equals, 1)
}

func (s *ModelsSuite) TestScheduleBuildCampaignLaunches(c *check.C) {
	sched := s.newSchedule(c)
	sched.SendByOffset = 2
	launch := time.Now().UTC()

	built := sched.BuildCampaign(launch)
	c.Assert(built.Template.Name, check.Equals, sched.TemplateName)
	c.Assert(built.Page.Name, check.Equals, sched.PageName)
	c.Assert(built.SMTP.Name, check.Equals, sched.SMTPName)
	c.Assert(len(built.Groups), check.Equals, 1)
	c.Assert(built.SendByDate.Equal(launch.Add(2*time.Minute)), check.Equals, true)

	// The built campaign is accepted by PostCampaign and spawns maillogs.
	c.Assert(PostCampaign(&built, sched.UserId, 1), check.Equals, nil)
	ms, err := GetMailLogsByCampaign(built.Id)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(ms) > 0, check.Equals, true)
}
