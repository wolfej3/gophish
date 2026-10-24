package sender

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gophish/gophish/mailer"
	"golang.org/x/oauth2/clientcredentials"
)

// graphBaseURL is Microsoft Graph's default base address.
const graphBaseURL = "https://graph.microsoft.com/v1.0"

// GraphConfig configures a Microsoft Graph sender. The app registration needs
// the Mail.Send application permission. From is the mailbox to send as.
type GraphConfig struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	From         string
	// BaseURL and TokenURL override the Graph and token endpoints, for
	// testing or sovereign clouds. Empty uses the public endpoints.
	BaseURL  string
	TokenURL string
}

// NewGraphDialer returns a mailer.Dialer that sends through Microsoft Graph.
func NewGraphDialer(c GraphConfig) (mailer.Dialer, error) {
	switch {
	case c.TenantID == "":
		return nil, errors.New("graph: tenant_id is required")
	case c.ClientID == "":
		return nil, errors.New("graph: client_id is required")
	case c.ClientSecret == "":
		return nil, errors.New("graph: client_secret is required")
	case c.From == "":
		return nil, errors.New("graph: a from address is required")
	}
	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", c.TenantID)
	}
	cc := &clientcredentials.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		TokenURL:     tokenURL,
		Scopes:       []string{"https://graph.microsoft.com/.default"},
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = graphBaseURL
	}
	client := cc.Client(context.Background())
	client.Timeout = apiTimeout
	return staticDialer{sender: &graphSender{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		from:    c.From,
	}}, nil
}

// graphSender sends messages through Microsoft Graph's sendMail endpoint using
// the raw MIME format.
type graphSender struct {
	noopCloser
	client  *http.Client
	baseURL string
	from    string
}

func (s *graphSender) Send(from string, to []string, msg io.WriterTo) error {
	mime, err := mimeBytes(msg)
	if err != nil {
		return err
	}
	// Graph accepts a base64-encoded MIME message posted as text/plain to
	// the sender's sendMail endpoint.
	body := base64.StdEncoding.EncodeToString(mime)
	endpoint := fmt.Sprintf("%s/users/%s/sendMail", s.baseURL, url.PathEscape(s.from))
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		var detail bytes.Buffer
		io.CopyN(&detail, resp.Body, 2048)
		return fmt.Errorf("graph: sendMail returned %s: %s", resp.Status, strings.TrimSpace(detail.String()))
	}
	return nil
}
