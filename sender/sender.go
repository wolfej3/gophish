// Package sender provides mailer transports that send campaign email through
// provider APIs (Microsoft Graph and Gmail) instead of SMTP. Each implements
// the mailer.Dialer and mailer.Sender interfaces, consuming the same RFC 822
// message the SMTP path produces.
package sender

import (
	"bytes"
	"io"
	"time"

	"github.com/gophish/gophish/mailer"
)

// apiTimeout bounds each API request.
const apiTimeout = 30 * time.Second

// mimeBytes renders a mailer message to its raw RFC 822 bytes.
func mimeBytes(msg io.WriterTo) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := msg.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// noopCloser provides the Close and Reset methods of mailer.Sender for API
// senders, which hold no persistent connection.
type noopCloser struct{}

func (noopCloser) Close() error { return nil }
func (noopCloser) Reset() error { return nil }

// staticDialer returns the same sender every time Dial is called, since API
// senders don't hold a connection to reconnect.
type staticDialer struct {
	sender mailer.Sender
}

func (d staticDialer) Dial() (mailer.Sender, error) {
	return d.sender, nil
}
