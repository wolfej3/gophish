package models

import (
	"io"
	"strings"

	"github.com/gophish/gomail"
	"rsc.io/qr"
)

// qrCID is the Content-ID (and filename) of the inline QR image embedded in an
// email. Templates reference it as cid:gophish-qr.png.
const qrCID = "gophish-qr.png"

// qrCIDRef is the src a template uses to display the embedded QR image. The
// QR template field is a fixed <img> tag pointing here; the per-recipient PNG
// is generated and embedded at send time.
const qrCIDRef = "cid:" + qrCID

// qrPNG returns a PNG image of a QR code encoding text. It mirrors
// auth.TOTPQRCode, duplicated here to avoid a models -> auth import cycle.
func qrPNG(text string) ([]byte, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	// code.Image() doesn't apply the scale when drawing, so we use the
	// library's own PNG encoder, which does.
	code.Scale = 6
	return code.PNG(), nil
}

// embedQR embeds a QR image encoding the recipient's tracked URL into msg, but
// only when the rendered HTML actually references it (cid:gophish-qr.png). An
// email that doesn't use {{.QR}} is left byte-for-byte unchanged. Because every
// transport serializes the same gomail.Message, embedding here covers SMTP,
// Microsoft Graph, and Gmail alike.
func embedQR(msg *gomail.Message, html string, ptx PhishingTemplateContext) error {
	if !strings.Contains(html, qrCIDRef) {
		return nil
	}
	png, err := qrPNG(ptx.URL)
	if err != nil {
		return err
	}
	msg.Embed(qrCID, gomail.SetCopyFunc(func(w io.Writer) error {
		_, err := w.Write(png)
		return err
	}))
	return nil
}
