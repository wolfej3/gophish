package models

import (
	"strings"

	check "gopkg.in/check.v1"
)

func (s *ModelsSuite) TestSMTPValidateSMS(c *check.C) {
	valid := SMTP{
		Interface:   SMSInterface,
		SMSProvider: SMSProviderTwilio,
		SMSFrom:     "+15550000000",
		Username:    "AC_sid",
		Password:    "auth_token",
	}
	// An SMS profile validates without an email From address.
	c.Assert(valid.Validate(), check.Equals, nil)

	badProvider := valid
	badProvider.SMSProvider = "nexmo"
	c.Assert(badProvider.Validate(), check.Equals, ErrSMSProviderNotSupported)

	noFrom := valid
	noFrom.SMSFrom = ""
	c.Assert(noFrom.Validate(), check.Equals, ErrSMSFromNotSpecified)

	noCreds := valid
	noCreds.Username = ""
	c.Assert(noCreds.Validate(), check.Equals, ErrSMSCredentials)
}

// createSMSCampaign builds and posts an SMS campaign with a single phone
// recipient and the given template text.
func (s *ModelsSuite) createSMSCampaign(c *check.C, text, phone string) Campaign {
	profile := SMTP{
		Name:        "Twilio Profile",
		Interface:   SMSInterface,
		SMSProvider: SMSProviderTwilio,
		SMSFrom:     "+15550000000",
		Username:    "AC_sid",
		Password:    "auth_token",
		UserId:      1,
		TeamId:      1,
	}
	c.Assert(PostSMTP(&profile), check.Equals, nil)

	group := Group{Name: "SMS Group", UserId: 1, TeamId: 1}
	group.Targets = []Target{
		{BaseRecipient: BaseRecipient{Email: "sms@example.com", FirstName: "Pat", LastName: "Target", Phone: phone}},
	}
	c.Assert(PostGroup(&group), check.Equals, nil)

	tmpl := Template{Name: "SMS Template", Text: text, UserId: 1, TeamId: 1}
	c.Assert(PostTemplate(&tmpl), check.Equals, nil)

	page := Page{Name: "SMS Page", HTML: "<html>hi</html>", UserId: 1, TeamId: 1}
	c.Assert(PostPage(&page), check.Equals, nil)

	campaign := Campaign{Name: "SMS Campaign", UserId: 1, TeamId: 1, URL: "http://phish.example.com"}
	campaign.Template = tmpl
	campaign.Page = page
	campaign.SMTP = profile
	campaign.Groups = []Group{group}
	c.Assert(PostCampaign(&campaign, 1, 1), check.Equals, nil)
	return campaign
}

func (s *ModelsSuite) TestPostCampaignSMSRequiresText(c *check.C) {
	profile := SMTP{
		Name: "Twilio Profile", Interface: SMSInterface, SMSProvider: SMSProviderTwilio,
		SMSFrom: "+15550000000", Username: "AC", Password: "tok", UserId: 1, TeamId: 1,
	}
	c.Assert(PostSMTP(&profile), check.Equals, nil)
	group := Group{Name: "SMS Group", UserId: 1, TeamId: 1}
	group.Targets = []Target{{BaseRecipient: BaseRecipient{Email: "a@example.com", Phone: "+15551112222"}}}
	c.Assert(PostGroup(&group), check.Equals, nil)
	// HTML-only template, no text: invalid for SMS.
	tmpl := Template{Name: "No Text", HTML: "<html>x</html>", UserId: 1, TeamId: 1}
	c.Assert(PostTemplate(&tmpl), check.Equals, nil)
	page := Page{Name: "P", HTML: "<html>x</html>", UserId: 1, TeamId: 1}
	c.Assert(PostPage(&page), check.Equals, nil)

	campaign := Campaign{Name: "C", UserId: 1, TeamId: 1, URL: "http://phish.example.com"}
	campaign.Template = tmpl
	campaign.Page = page
	campaign.SMTP = profile
	campaign.Groups = []Group{group}
	c.Assert(PostCampaign(&campaign, 1, 1), check.Equals, ErrSMSTemplateText)
}

func (s *ModelsSuite) TestMailLogGenerateSMS(c *check.C) {
	campaign := s.createSMSCampaign(c, "Tap {{.URL}} to verify", "+15557654321")
	ms, err := GetMailLogsByCampaign(campaign.Id)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(ms), check.Equals, 1)

	to, body, err := ms[0].GenerateSMS()
	c.Assert(err, check.Equals, nil)
	c.Assert(to, check.Equals, "+15557654321")
	// The body uses the masked short link, not the ?rid= URL.
	c.Assert(strings.Contains(body, "/l/"+ms[0].RId), check.Equals, true)
	c.Assert(strings.Contains(body, "rid="), check.Equals, false)
	c.Assert(strings.HasPrefix(body, "Tap http://phish.example.com/l/"), check.Equals, true)
}
