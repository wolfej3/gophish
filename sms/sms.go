// Package sms sends SMS text messages through a provider's HTTP API. It's the
// SMS counterpart to the email mailer: given a short body and a destination
// number, it delivers the message. It deliberately has no dependency on the
// rest of Gophish so it can't create an import cycle, and uses net/http
// directly rather than a provider SDK.
package sms

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// requestTimeout bounds each provider API call.
const requestTimeout = 30 * time.Second

// Message is a single SMS to send.
type Message struct {
	To   string
	From string
	Body string
}

// Sender delivers SMS messages through a provider.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// ErrUnsupportedProvider is returned when no client exists for a provider name.
var ErrUnsupportedProvider = errors.New("unsupported SMS provider")

// New returns a Sender for the named provider configured with the given
// credentials. Currently only "twilio" is supported.
func New(provider, accountSID, authToken string) (Sender, error) {
	switch provider {
	case "twilio":
		return &twilioSender{
			accountSID: accountSID,
			authToken:  authToken,
			baseURL:    twilioBaseURL(),
			client:     &http.Client{Timeout: requestTimeout},
		}, nil
	default:
		return nil, ErrUnsupportedProvider
	}
}

// twilioBaseURL is the Twilio API root. It can be overridden with
// GOPHISH_SMS_BASE_URL to point at a self-hosted gateway or a test double; it
// defaults to the real Twilio endpoint.
func twilioBaseURL() string {
	if v := os.Getenv("GOPHISH_SMS_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.twilio.com"
}

// twilioSender sends through the Twilio REST API.
type twilioSender struct {
	accountSID string
	authToken  string
	baseURL    string
	client     *http.Client
}

// Send posts a message to Twilio's Messages endpoint. A non-2xx response is
// returned as an error including Twilio's message, so the caller can record a
// sending failure on the result.
func (t *twilioSender) Send(ctx context.Context, m Message) error {
	if m.To == "" {
		return errors.New("no destination phone number")
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", t.baseURL, url.PathEscape(t.accountSID))
	form := url.Values{}
	form.Set("To", m.To)
	form.Set("From", m.From)
	form.Set("Body", m.Body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.accountSID, t.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("sms provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
