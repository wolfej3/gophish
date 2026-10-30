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
	"golang.org/x/oauth2/jwt"
)

// gmailBaseURL is the Gmail API's default base address.
const gmailBaseURL = "https://gmail.googleapis.com/gmail/v1"

// gmailSendScope lets the service account send mail as the delegated user.
const gmailSendScope = "https://www.googleapis.com/auth/gmail.send"

// gmailInsertScope lets the service account insert mail into a delegated
// user's mailbox (direct inbox injection) without delivering it.
const gmailInsertScope = "https://www.googleapis.com/auth/gmail.insert"

// GmailConfig configures a Gmail sender. CredentialsFile is a service account
// key with domain-wide delegation for the Gmail send scope; From is the
// Workspace user to send as (also the delegated subject).
type GmailConfig struct {
	CredentialsFile string
	From            string
	// Insert selects direct inbox injection (DMI): messages are written
	// into each recipient's mailbox with gmail.insert instead of being
	// delivered with messages.send. It impersonates the recipient (not
	// From) and needs domain-wide delegation for the gmail.insert scope
	// over the recipients' Workspace mailboxes. From is only the lure's
	// From header in this mode.
	Insert bool
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
	scope := gmailSendScope
	if c.Insert {
		scope = gmailInsertScope
	}
	jwtConfig, err := google.JWTConfigFromJSON(data, scope)
	if err != nil {
		return nil, fmt.Errorf("gmail: parsing credentials: %w", err)
	}
	if c.TokenURL != "" {
		jwtConfig.TokenURL = c.TokenURL
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = gmailBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	// DMI impersonates the recipient, which differs per message, so the
	// delegated subject can't be fixed at dial time; the sender mints a
	// per-recipient client at send time.
	if c.Insert {
		return staticDialer{sender: &gmailInsertSender{
			jwtConfig: jwtConfig,
			baseURL:   baseURL,
		}}, nil
	}
	// Send as the delegated user through domain-wide delegation.
	jwtConfig.Subject = c.From
	client := jwtConfig.Client(context.Background())
	client.Timeout = apiTimeout
	return staticDialer{sender: &gmailSender{
		client:  client,
		baseURL: baseURL,
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

// gmailInsertSender writes messages straight into each recipient's mailbox
// through the Gmail API's messages.insert endpoint (direct inbox injection).
// Because insert impersonates the mailbox owner and the recipient differs per
// message, it mints a client per recipient at send time, using the recipient
// as the delegated subject.
type gmailInsertSender struct {
	noopCloser
	jwtConfig *jwt.Config
	baseURL   string
}

func (s *gmailInsertSender) Send(from string, to []string, msg io.WriterTo) error {
	mime, err := mimeBytes(msg)
	if err != nil {
		return err
	}
	// The MIME (with tracking/QR already baked in) is identical for every
	// recipient; only the delegated subject changes.
	payload, err := json.Marshal(map[string]interface{}{
		"raw":      base64.URLEncoding.EncodeToString(mime),
		"labelIds": []string{"INBOX", "UNREAD"},
	})
	if err != nil {
		return err
	}
	for _, recipient := range to {
		if err := s.insert(recipient, payload); err != nil {
			return err
		}
	}
	return nil
}

// insert writes the message into a single recipient's mailbox, impersonating
// that recipient as the delegated subject.
func (s *gmailInsertSender) insert(recipient string, payload []byte) error {
	cfg := *s.jwtConfig
	cfg.Subject = recipient
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	client := cfg.Client(ctx)
	client.Timeout = apiTimeout
	endpoint := s.baseURL + "/users/me/messages/insert"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var detail bytes.Buffer
		io.CopyN(&detail, resp.Body, 2048)
		return fmt.Errorf("gmail: messages.insert returned %s: %s", resp.Status, strings.TrimSpace(detail.String()))
	}
	return nil
}
