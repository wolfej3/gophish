package models

import (
	"gopkg.in/check.v1"
	"gorm.io/gorm"
)

// These tests guard behavior that changed between gorm v1 and v2 and isn't
// caught by the compiler.

// TestModelColumnsMatchSchema ensures every column gorm maps for our models
// exists in the database created by the migrations, since gorm's naming of
// fields like RId or SMTPId must match the columns the migrations created.
func (s *ModelsSuite) TestModelColumnsMatchSchema(c *check.C) {
	models := []interface{}{
		&User{}, &Role{}, &Permission{}, &Campaign{}, &Event{}, &Result{},
		&MailLog{}, &Group{}, &Target{}, &GroupTarget{}, &Template{},
		&Attachment{}, &Page{}, &SMTP{}, &Header{}, &IMAP{}, &Webhook{},
		&EmailRequest{},
	}
	migrator := db.Migrator()
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		c.Assert(stmt.Parse(model), check.Equals, nil)
		c.Assert(migrator.HasTable(model), check.Equals, true,
			check.Commentf("missing table %s", stmt.Schema.Table))
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" {
				continue
			}
			c.Assert(migrator.HasColumn(model, field.DBName), check.Equals, true,
				check.Commentf("missing column %s.%s", stmt.Schema.Table, field.DBName))
		}
	}
}

// TestGetObjectsScopedToOwner ensures lookups for another user's objects
// return ErrRecordNotFound. The API relies on this error to stop users from
// reading, modifying, or deleting objects they don't own.
func (s *ModelsSuite) TestGetObjectsScopedToOwner(c *check.C) {
	campaign := s.createCampaign(c)
	otherUID := campaign.UserId + 1

	_, err := GetCampaign(campaign.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetCampaignMailContext(campaign.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetCampaignResults(campaign.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetCampaignSummary(campaign.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetTemplate(campaign.Template.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetTemplateByName(campaign.Template.Name, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetPage(campaign.Page.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetPageByName(campaign.Page.Name, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetSMTP(campaign.SMTP.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetSMTPByName(campaign.SMTP.Name, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)

	group, err := GetGroupByName("Test Group", campaign.UserId)
	c.Assert(err, check.Equals, nil)
	_, err = GetGroup(group.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetGroupByName(group.Name, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetGroupSummary(group.Id, otherUID)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
}

// TestCampaignDeletedDependencies ensures a campaign whose template, page,
// or sending profile was deleted reports them as "[Deleted]".
func (s *ModelsSuite) TestCampaignDeletedDependencies(c *check.C) {
	campaign := s.createCampaign(c)
	c.Assert(DeleteTemplate(campaign.Template.Id, campaign.UserId), check.Equals, nil)
	c.Assert(DeletePage(campaign.Page.Id, campaign.UserId), check.Equals, nil)
	c.Assert(DeleteSMTP(campaign.SMTP.Id, campaign.UserId), check.Equals, nil)

	got, err := GetCampaign(campaign.Id, campaign.UserId)
	c.Assert(err, check.Equals, nil)
	c.Assert(got.Template.Name, check.Equals, "[Deleted]")
	c.Assert(got.Page.Name, check.Equals, "[Deleted]")
	c.Assert(got.SMTP.Name, check.Equals, "[Deleted]")
}

// TestCampaignStats ensures each count in the campaign stats is computed
// independently of the others.
func (s *ModelsSuite) TestCampaignStats(c *check.C) {
	campaign := s.createCampaign(c)
	c.Assert(campaign.Results, check.HasLen, 4)
	statuses := []struct {
		status   string
		reported bool
	}{
		{EventDataSubmit, false},
		{EventClicked, false},
		{EventOpened, true},
		{EventSent, false},
	}
	for i, st := range statuses {
		err := db.Model(&Result{}).Where("id = ?", campaign.Results[i].Id).
			Updates(map[string]interface{}{"status": st.status, "reported": st.reported}).Error
		c.Assert(err, check.Equals, nil)
	}

	stats, err := getCampaignStats(campaign.Id)
	c.Assert(err, check.Equals, nil)
	c.Assert(stats, check.DeepEquals, CampaignStats{
		Total:         4,
		SubmittedData: 1,
		// Submitting data implies a click, a click implies an open, and an
		// open implies the email was sent.
		ClickedLink:   2,
		OpenedEmail:   3,
		EmailsSent:    4,
		EmailReported: 1,
		Error:         0,
	})
}
