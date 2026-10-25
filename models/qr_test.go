package models

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	"github.com/gophish/gomail"
	"github.com/jordan-wright/email"
	"gopkg.in/check.v1"
)

// findInlinePNG walks a raw MIME message and returns the decoded content of the
// first image/png part, or nil if there isn't one.
func findInlinePNG(raw string) []byte {
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	return walkForPNG(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
}

func walkForPNG(contentType, encoding string, body io.Reader) []byte {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				return nil
			}
			if found := walkForPNG(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part); found != nil {
				return found
			}
		}
	}
	if strings.HasPrefix(mediaType, "image/png") {
		data, err := io.ReadAll(body)
		if err != nil {
			return nil
		}
		if strings.EqualFold(encoding, "base64") {
			decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(data)), ""))
			if err != nil {
				return nil
			}
			return decoded
		}
		return data
	}
	return nil
}

func (s *ModelsSuite) TestQRPNGIsValid(c *check.C) {
	b, err := qrPNG("http://example.com/?rid=abc123")
	c.Assert(err, check.Equals, nil)
	c.Assert(len(b) > 0, check.Equals, true)
	_, err = png.Decode(bytes.NewReader(b))
	c.Assert(err, check.Equals, nil)
}

// qrTemplateCampaign returns a campaign whose email template HTML is the given
// body, so tests can control whether {{.QR}} is referenced.
func (s *ModelsSuite) qrTemplateCampaign(c *check.C, html string) Campaign {
	campaign := s.createCampaignDependencies(c)
	tmpl := Template{
		Name:    "QR Template",
		Subject: "Subject",
		HTML:    html,
		UserId:  1,
		TeamId:  1,
	}
	c.Assert(PostTemplate(&tmpl), check.Equals, nil)
	campaign.Template = tmpl
	campaign.Name = "QR Campaign"
	c.Assert(PostCampaign(&campaign, campaign.UserId, 1), check.Equals, nil)
	return campaign
}

// rawAndParsed generates the first maillog's MIME and returns both the raw
// bytes and the parsed email.
func (s *ModelsSuite) rawMailLog(c *check.C, campaign Campaign) (string, *email.Email) {
	result := campaign.Results[0]
	m := &MailLog{}
	c.Assert(db.Where("r_id=? AND campaign_id=?", result.RId, campaign.Id).Find(m).Error, check.Equals, nil)
	msg := gomail.NewMessage()
	c.Assert(m.Generate(msg), check.Equals, nil)
	buf := &bytes.Buffer{}
	_, err := msg.WriteTo(buf)
	c.Assert(err, check.Equals, nil)
	raw := buf.String()
	parsed, err := email.NewEmailFromReader(bytes.NewReader(buf.Bytes()))
	c.Assert(err, check.Equals, nil)
	return raw, parsed
}

// assertInlineQR checks that the message carries an inline PNG whose Content-ID
// matches the QR CID, and that the PNG decodes.
func assertInlineQR(c *check.C, raw string, parsed *email.Email) {
	// The message carries the QR's Content-ID and an inline PNG part.
	c.Assert(strings.Contains(raw, qrCID), check.Equals, true)
	// The rendered HTML references the embedded image by CID.
	c.Assert(strings.Contains(string(parsed.HTML), qrCIDRef), check.Equals, true)
	// The inline PNG decodes as a valid image.
	pngBytes := findInlinePNG(raw)
	c.Assert(pngBytes, check.NotNil)
	_, err := png.Decode(bytes.NewReader(pngBytes))
	c.Assert(err, check.Equals, nil)
}

func (s *ModelsSuite) TestMailLogEmbedsQRWhenReferenced(c *check.C) {
	campaign := s.qrTemplateCampaign(c, "<html><body>Scan: {{.QR}}</body></html>")
	raw, parsed := s.rawMailLog(c, campaign)
	assertInlineQR(c, raw, parsed)
}

func (s *ModelsSuite) TestMailLogNoQRWhenNotReferenced(c *check.C) {
	campaign := s.qrTemplateCampaign(c, "<html><body>No QR here {{.URL}}</body></html>")
	raw, parsed := s.rawMailLog(c, campaign)
	c.Assert(strings.Contains(raw, qrCID), check.Equals, false)
	for _, a := range parsed.Attachments {
		c.Assert(strings.Contains(a.Header.Get("Content-ID"), qrCID), check.Equals, false)
	}
}

func (s *ModelsSuite) TestEmailRequestEmbedsQR(c *check.C) {
	req := &EmailRequest{
		SMTP: SMTP{FromAddress: "from@example.com"},
		Template: Template{
			Name:    "QR Test Send",
			Subject: "Subject",
			HTML:    "<html><body>Scan: {{.QR}}</body></html>",
		},
		URL:         "http://localhost.localdomain",
		FromAddress: "from@example.com",
		BaseRecipient: BaseRecipient{
			Email:     "test@example.com",
			FirstName: "First",
			LastName:  "Last",
		},
	}

	msg := gomail.NewMessage()
	c.Assert(req.Generate(msg), check.Equals, nil)
	buf := &bytes.Buffer{}
	_, err := msg.WriteTo(buf)
	c.Assert(err, check.Equals, nil)
	parsed, err := email.NewEmailFromReader(bytes.NewReader(buf.Bytes()))
	c.Assert(err, check.Equals, nil)
	assertInlineQR(c, buf.String(), parsed)
}
