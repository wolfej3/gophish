package sender

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gophish/gophish/mailer"
	"golang.org/x/oauth2/google"
)

// gmailBaseURL is the Gmail API's default base address.
const gmailBaseURL = "https://gmail.googleapis.com/gmail/v1"

// gmailSendScope lets the service account send mail as the delegated user.
const gmailSendScope = "https://www.googleapis.com/auth/gmail.send"

// GmailConfig configures a Gmail sender. CredentialsFile is a service account
// key with domain-wide delegation for the Gmail send scope; From is the
// Workspace user to send as (also the delegated subject).
type GmailConfig struct {
	CredentialsFile string
	From            string
	// BaseURL and TokenURL override the API and token endpoints, for
	// testing. Empty uses the public endpoints (the token endpoint then
	// comes from the service account key).
	BaseURL  string
	TokenURL string
}

// NewGmailDialer returns a mailer.Dialer that sends through the Gmail API.
func NewGmailDialer(c GmailConfig) (mailer.Dialer, error) {
	if c.CredentialsFile == "" {
		return nil, errors.New("gmail: credentials_file is required")
	}
	if c.From == "" {
		return nil, errors.New("gmail: a from address is required")
	}
	data, err := os.ReadFile(c.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("gmail: reading credentials: %w", err)
	}
	jwtConfig, err := google.JWTConfigFromJSON(data, gmailSendScope)
	if err != nil {
		return nil, fmt.Errorf("gmail: parsing credentials: %w", err)
	}
	// Send as the delegated user through domain-wide delegation.
	jwtConfig.Subject = c.From
	if c.TokenURL != "" {
		jwtConfig.TokenURL = c.TokenURL
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = gmailBaseURL
	}
	client := jwtConfig.Client(context.Background())
	client.Timeout = apiTimeout
	return staticDialer{sender: &gmailSender{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
	}}, nil
}

// gmailSender sends messages through the Gmail API's messages.send endpoint.
type gmailSender struct {
	noopCloser
	client  *http.Client
	baseURL string
}

func (s *gmailSender) Send(from string, to []string, msg io.WriterTo) error {
	mime, err := mimeBytes(msg)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{
		"raw": base64.URLEncoding.EncodeToString(mime),
	})
	if err != nil {
		return err
	}
	endpoint := s.baseURL + "/users/me/messages/send"
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var detail bytes.Buffer
		io.CopyN(&detail, resp.Body, 2048)
		return fmt.Errorf("gmail: messages.send returned %s: %s", resp.Status, strings.TrimSpace(detail.String()))
	}
	return nil
}
