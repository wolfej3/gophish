package models

import (
	"bytes"
	"errors"
	"net/mail"
	"net/url"
	"path"
	"text/template"
)

// TemplateContext is an interface that allows both campaigns and email
// requests to have a PhishingTemplateContext generated for them.
type TemplateContext interface {
	getFromAddress() string
	getBaseURL() string
}

// PhishingTemplateContext is the context that is sent to any template, such
// as the email or landing page content.
type PhishingTemplateContext struct {
	From        string
	URL         string
	Tracker     string
	QR          string
	TrackingURL string
	RId         string
	BaseURL     string
	BaseRecipient
}

// NewPhishingTemplateContext returns a populated PhishingTemplateContext,
// parsing the correct fields from the provided TemplateContext and recipient.
func NewPhishingTemplateContext(ctx TemplateContext, r BaseRecipient, rid string) (PhishingTemplateContext, error) {
	f, err := mail.ParseAddress(ctx.getFromAddress())
	if err != nil {
		return PhishingTemplateContext{}, err
	}
	fn := f.Name
	if fn == "" {
		fn = f.Address
	}
	templateURL, err := ExecuteTemplate(ctx.getBaseURL(), r)
	if err != nil {
		return PhishingTemplateContext{}, err
	}

	// For the base URL, we'll reset the the path and the query
	// This will create a URL in the form of http://example.com
	baseURL, err := url.Parse(templateURL)
	if err != nil {
		return PhishingTemplateContext{}, err
	}
	baseURL.Path = ""
	baseURL.RawQuery = ""

	phishURL, _ := url.Parse(templateURL)
	q := phishURL.Query()
	q.Set(RecipientParameter, rid)
	phishURL.RawQuery = q.Encode()

	trackingURL, _ := url.Parse(templateURL)
	trackingURL.Path = path.Join(trackingURL.Path, "/track")
	trackingURL.RawQuery = q.Encode()

	return PhishingTemplateContext{
		BaseRecipient: r,
		BaseURL:       baseURL.String(),
		URL:           phishURL.String(),
		TrackingURL:   trackingURL.String(),
		Tracker:       "<img alt='' style='display: none' src='" + trackingURL.String() + "'/>",
		QR:            "<img alt='' src='" + qrCIDRef + "'/>",
		From:          fn,
		RId:           rid,
	}, nil
}

// ExecuteTemplate creates a templated string based on the provided
// template body and data.
// maxTemplateOutput caps a rendered template's size. Since Go 1.22 text
// templates support {{range N}}, so a template could otherwise expand without
// bound and exhaust memory when rendered per recipient. It's a var so tests
// can lower it.
var maxTemplateOutput = 25 << 20 // 25 MB

// ErrTemplateTooLarge is returned when a template renders more than
// maxTemplateOutput bytes.
var ErrTemplateTooLarge = errors.New("Template output is too large")

func ExecuteTemplate(text string, data interface{}) (string, error) {
	tmpl, err := template.New("template").Parse(text)
	if err != nil {
		return "", err
	}
	cw := &cappedWriter{limit: maxTemplateOutput}
	if err := tmpl.Execute(cw, data); err != nil {
		if cw.exceeded {
			return "", ErrTemplateTooLarge
		}
		return "", err
	}
	return cw.buf.String(), nil
}

// cappedWriter collects template output up to limit bytes, then fails so
// execution stops instead of buffering without bound.
type cappedWriter struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.limit {
		c.exceeded = true
		return 0, ErrTemplateTooLarge
	}
	return c.buf.Write(p)
}

// ValidationContext is used for validating templates and pages
type ValidationContext struct {
	FromAddress string
	BaseURL     string
}

func (vc ValidationContext) getFromAddress() string {
	return vc.FromAddress
}

func (vc ValidationContext) getBaseURL() string {
	return vc.BaseURL
}

// ValidateTemplate ensures that the provided text in the page or template
// uses the supported template variables correctly.
func ValidateTemplate(text string) error {
	vc := ValidationContext{
		FromAddress: "foo@bar.com",
		BaseURL:     "http://example.com",
	}
	td := Result{
		BaseRecipient: BaseRecipient{
			Email:     "foo@bar.com",
			FirstName: "Foo",
			LastName:  "Bar",
			Position:  "Test",
		},
		RId: "123456",
	}
	ptx, err := NewPhishingTemplateContext(vc, td.BaseRecipient, td.RId)
	if err != nil {
		return err
	}
	_, err = ExecuteTemplate(text, ptx)
	if err != nil {
		return err
	}
	return nil
}
