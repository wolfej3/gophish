package auth

import (
	"bytes"
	"encoding/base32"
	"image/png"
	"testing"
	"time"
)

// rfcSecret is the SHA-1 key from the RFC 6238 test vectors.
var rfcSecret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

func TestTOTPCodeRFCVectors(t *testing.T) {
	// The RFC lists 8 digit codes; ours are the last 6 digits of each.
	vectors := map[int64]string{
		59:         "287082",
		1111111109: "081804",
		1111111111: "050471",
		1234567890: "005924",
		2000000000: "279037",
	}
	for ts, expected := range vectors {
		got, err := TOTPCode(rfcSecret, time.Unix(ts, 0))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != expected {
			t.Fatalf("unexpected code at %d. expected %s got %s", ts, expected, got)
		}
	}
}

func TestValidateTOTP(t *testing.T) {
	secret := GenerateTOTPSecret()
	now := time.Now()
	code, _ := TOTPCode(secret, now)
	step, err := ValidateTOTP(secret, code, now, 0)
	if err != nil {
		t.Fatalf("expected valid code, got error: %v", err)
	}
	if step != now.Unix()/totpPeriod {
		t.Fatalf("unexpected step. expected %d got %d", now.Unix()/totpPeriod, step)
	}
	// The same code can't be used again
	if _, err := ValidateTOTP(secret, code, now, step); err != ErrInvalidTOTPCode {
		t.Fatalf("expected replayed code to be rejected, got %v", err)
	}
	// Codes from one period ago are accepted for clock drift, but not older
	prev, _ := TOTPCode(secret, now.Add(-totpPeriod*time.Second))
	if _, err := ValidateTOTP(secret, prev, now, 0); err != nil {
		t.Fatalf("expected previous code to be accepted, got %v", err)
	}
	old, _ := TOTPCode(secret, now.Add(-5*totpPeriod*time.Second))
	if old != code && old != prev {
		if _, err := ValidateTOTP(secret, old, now, 0); err != ErrInvalidTOTPCode {
			t.Fatalf("expected old code to be rejected, got %v", err)
		}
	}
	for _, bad := range []string{"", "12345", "abcdef", "1234567"} {
		if _, err := ValidateTOTP(secret, bad, now, 0); err != ErrInvalidTOTPCode {
			t.Fatalf("expected %q to be rejected, got %v", bad, err)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := GenerateRecoveryCodes()
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("unexpected number of codes. expected %d got %d", RecoveryCodeCount, len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Fatalf("unexpected recovery code format: %q", c)
		}
		if seen[c] {
			t.Fatalf("duplicate recovery code: %q", c)
		}
		seen[c] = true
		if got := NormalizeRecoveryCode(" " + c[:5] + " " + c[6:] + " "); got != c {
			t.Fatalf("unexpected normalized code. expected %q got %q", c, got)
		}
	}
}

func TestTOTPQRCodeIsScaled(t *testing.T) {
	data, err := TOTPQRCode(TOTPURI("admin", GenerateTOTPSecret()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("error decoding QR code: %v", err)
	}
	// The code should fill the image, so there should be dark modules in its
	// bottom right quarter, not just near the top left corner.
	b := img.Bounds()
	dark := 0
	for y := b.Max.Y / 2; y < b.Max.Y; y++ {
		for x := b.Max.X / 2; x < b.Max.X; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Fatal("expected the QR code to be drawn across the whole image")
	}
}
