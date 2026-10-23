package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

// TOTPIssuer is the issuer shown in authenticator apps for Gophish accounts.
const TOTPIssuer = "Gophish"

// totpPeriod is the number of seconds each TOTP code is valid for.
const totpPeriod = 30

// totpDigits is the number of digits in a TOTP code.
const totpDigits = 6

// totpSkew is the number of periods before and after the current one in which
// we still accept a code, to allow for clock drift between the server and
// the user's device.
const totpSkew = 1

// RecoveryCodeCount is the number of recovery codes issued to a user when
// they enable two-factor authentication.
const RecoveryCodeCount = 10

// ErrInvalidTOTPCode is thrown when a user provides an incorrect, expired, or
// already used two-factor code.
var ErrInvalidTOTPCode = errors.New("Invalid two-factor code")

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a new random 160-bit TOTP secret, encoded in
// base32 as authenticator apps expect.
func GenerateTOTPSecret() string {
	k := make([]byte, 20)
	io.ReadFull(rand.Reader, k)
	return base32NoPadding.EncodeToString(k)
}

// TOTPURI returns the otpauth:// URI used to add the secret to an
// authenticator app for the given account.
func TOTPURI(account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", TOTPIssuer)
	label := url.PathEscape(fmt.Sprintf("%s:%s", TOTPIssuer, account))
	return fmt.Sprintf("otpauth://totp/%s?%s", label, v.Encode())
}

// TOTPQRCode returns a PNG image of a QR code encoding the provided otpauth://
// URI, for scanning with an authenticator app.
func TOTPQRCode(uri string) ([]byte, error) {
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return nil, err
	}
	// code.Image() doesn't apply the scale when drawing, so we use the
	// library's own PNG encoder, which does.
	code.Scale = 6
	return code.PNG(), nil
}

// totpCode returns the TOTP code for the secret at the given time step, as
// defined in RFC 6238.
func totpCode(key []byte, step int64) string {
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1000000)
}

// TOTPCode returns the TOTP code for the secret at the given time.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := base32NoPadding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	return totpCode(key, t.Unix()/totpPeriod), nil
}

// ValidateTOTP checks the provided code against the secret at the given time.
// To prevent a code from being used twice, codes for time steps at or before
// lastStep are rejected. On success, the time step that matched is returned so
// that the caller can store it as the new lastStep.
func ValidateTOTP(secret, code string, t time.Time, lastStep int64) (int64, error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, ErrInvalidTOTPCode
	}
	key, err := base32NoPadding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return 0, err
	}
	current := t.Unix() / totpPeriod
	for step := current - totpSkew; step <= current+totpSkew; step++ {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(key, step)), []byte(code)) == 1 {
			return step, nil
		}
	}
	return 0, ErrInvalidTOTPCode
}

// GenerateRecoveryCodes returns RecoveryCodeCount new single-use recovery
// codes, formatted as two groups of five characters (e.g. "abcde-fghij").
func GenerateRecoveryCodes() []string {
	codes := make([]string, RecoveryCodeCount)
	for i := range codes {
		k := make([]byte, 7)
		io.ReadFull(rand.Reader, k)
		c := strings.ToLower(base32NoPadding.EncodeToString(k))[:10]
		codes[i] = c[:5] + "-" + c[5:]
	}
	return codes
}

// NormalizeRecoveryCode puts a recovery code typed by a user into the form
// it was issued in, so that differences in case or spacing don't matter.
func NormalizeRecoveryCode(code string) string {
	code = strings.ToLower(strings.Join(strings.Fields(code), ""))
	code = strings.ReplaceAll(code, "-", "")
	if len(code) != 10 {
		return code
	}
	return code[:5] + "-" + code[5:]
}
