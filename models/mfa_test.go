package models

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"gopkg.in/check.v1"
)

// TestMFAImpliesCaptureCredentials ensures that enabling the MFA lure
// automatically enables credential capture, since the verification-code step
// only runs after the credential form is submitted.
func (s *ModelsSuite) TestMFAImpliesCaptureCredentials(c *check.C) {
	p := Page{
		Name:       "MFA Page",
		HTML:       "<html><body><form><input name=\"username\"/></form></body></html>",
		CaptureMFA: true,
	}
	p.UserId = 1
	p.TeamId = 1
	c.Assert(PostPage(&p), check.Equals, nil)
	c.Assert(p.CaptureCredentials, check.Equals, true)
	c.Assert(p.CaptureMFA, check.Equals, true)
}

// TestMFAPromptRewritesForm ensures a custom verification-code prompt is
// rewritten on save so its form posts back to Gophish (action cleared) and
// carries the hidden marker that distinguishes the code step.
func (s *ModelsSuite) TestMFAPromptRewritesForm(c *check.C) {
	p := Page{
		Name:       "MFA Prompt Page",
		HTML:       "<html><body><form><input name=\"username\"/></form></body></html>",
		CaptureMFA: true,
		MFAPrompt:  "<html><body><form action=\"https://real.example.com/verify\"><input name=\"mfa_code\"/></form></body></html>",
	}
	p.UserId = 1
	p.TeamId = 1
	c.Assert(PostPage(&p), check.Equals, nil)

	d, err := goquery.NewDocumentFromReader(strings.NewReader(p.MFAPrompt))
	c.Assert(err, check.Equals, nil)
	f := d.Find("form")
	action, _ := f.Attr("action")
	c.Assert(action, check.Equals, "")
	marker := f.Find("input[name=\"" + MFAMarkerField + "\"]")
	c.Assert(marker.Length(), check.Equals, 1)
	val, _ := marker.Attr("value")
	c.Assert(val, check.Equals, "1")
	// The author's code field is untouched.
	c.Assert(f.Find("input[name=\"mfa_code\"]").Length(), check.Equals, 1)
}

// TestMFAPromptMarkerNotDuplicated ensures a prompt that already includes the
// marker isn't given a second one.
func (s *ModelsSuite) TestMFAPromptMarkerNotDuplicated(c *check.C) {
	p := Page{
		Name:       "MFA Prompt Marker Page",
		HTML:       "<html><body><form><input name=\"username\"/></form></body></html>",
		CaptureMFA: true,
		MFAPrompt:  "<html><body><form><input type=\"hidden\" name=\"" + MFAMarkerField + "\" value=\"1\"/><input name=\"mfa_code\"/></form></body></html>",
	}
	p.UserId = 1
	p.TeamId = 1
	c.Assert(PostPage(&p), check.Equals, nil)
	d, err := goquery.NewDocumentFromReader(strings.NewReader(p.MFAPrompt))
	c.Assert(err, check.Equals, nil)
	c.Assert(d.Find("input[name=\""+MFAMarkerField+"\"]").Length(), check.Equals, 1)
}

// TestHandleMFASubmit ensures an MFA code submission is recorded as its own
// event, keeps the result status at Submitted Data, and is reflected in the
// campaign's SubmittedMFA stat.
func (s *ModelsSuite) TestHandleMFASubmit(ch *check.C) {
	c := s.createCampaign(ch)
	result := c.Results[0]

	details := EventDetails{
		Payload: url.Values{
			"username":     []string{"victim"},
			"password":     []string{"hunter2"},
			MFAMarkerField: []string{"1"},
			"mfa_code":     []string{"123456"},
		},
		Browser: map[string]string{"address": "127.0.0.1"},
	}
	// The credential submit happens first, then the code step.
	ch.Assert(result.HandleFormSubmit(EventDetails{Payload: url.Values{"username": []string{"victim"}}}), check.Equals, nil)
	ch.Assert(result.HandleMFASubmit(details), check.Equals, nil)

	// The top-level status stays at Submitted Data so the existing funnel is
	// unaffected.
	ch.Assert(result.Status, check.Equals, EventDataSubmit)

	// A distinct MFA event was recorded for this recipient.
	var mfaEvents int64
	ch.Assert(db.Table("events").
		Where("campaign_id = ? AND email = ? AND message = ?", c.Id, result.Email, EventMFASubmit).
		Count(&mfaEvents).Error, check.Equals, nil)
	ch.Assert(mfaEvents, check.Equals, int64(1))

	// The campaign stats count it as both a data submit and an MFA submit.
	stats, err := getCampaignStats(c.Id)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(stats.SubmittedData, check.Equals, int64(1))
	ch.Assert(stats.SubmittedMFA, check.Equals, int64(1))
}
