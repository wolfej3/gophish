package encryption

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withKey sets a fresh random key for the duration of a test and restores the
// previous state afterward.
func withKey(t *testing.T) {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	mu.RLock()
	prev := key
	mu.RUnlock()
	if err := SetKey(k); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	t.Cleanup(func() { SetKey(prev) })
}

func TestEncryptRoundTrip(t *testing.T) {
	withKey(t)
	for _, plaintext := range []string{"hunter2", "a longer secret with spaces and symbols !@#$%^&*()", "unicode: café — ☕"} {
		enc, err := Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if !IsEncrypted(enc) {
			t.Errorf("expected %q to carry the encrypted prefix", enc)
		}
		if strings.Contains(enc, plaintext) {
			t.Errorf("ciphertext %q leaks the plaintext", enc)
		}
		dec, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != plaintext {
			t.Errorf("round trip mismatch: got %q, want %q", dec, plaintext)
		}
	}
}

func TestEncryptNonDeterministic(t *testing.T) {
	withKey(t)
	a, _ := Encrypt("same")
	b, _ := Encrypt("same")
	if a == b {
		t.Error("two encryptions of the same plaintext should differ (random nonce)")
	}
}

func TestEncryptEmptyAndIdempotent(t *testing.T) {
	withKey(t)
	if got, _ := Encrypt(""); got != "" {
		t.Errorf("Encrypt(\"\") = %q, want empty", got)
	}
	enc, _ := Encrypt("secret")
	again, _ := Encrypt(enc)
	if again != enc {
		t.Errorf("Encrypt of an already-encrypted value changed it: %q -> %q", enc, again)
	}
}

func TestDecryptPlaintextPassthrough(t *testing.T) {
	withKey(t)
	// A value written before encryption was enabled has no prefix and must
	// read back unchanged.
	if got, err := Decrypt("legacy-plaintext"); err != nil || got != "legacy-plaintext" {
		t.Errorf("Decrypt(plaintext) = %q, %v; want passthrough", got, err)
	}
}

func TestDisabledIsNoOp(t *testing.T) {
	mu.RLock()
	prev := key
	mu.RUnlock()
	SetKey(nil)
	t.Cleanup(func() { SetKey(prev) })

	if Enabled() {
		t.Fatal("expected encryption disabled with a nil key")
	}
	enc, err := Encrypt("secret")
	if err != nil || enc != "secret" {
		t.Errorf("disabled Encrypt should pass through: got %q, %v", enc, err)
	}
	// An encrypted value cannot be read without a key.
	if _, err := Decrypt("enc:v1:whatever"); err != ErrNoKey {
		t.Errorf("expected ErrNoKey decrypting without a key, got %v", err)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	withKey(t)
	enc, _ := Encrypt("secret")
	// Swap in a different key; decryption must fail rather than return garbage.
	k2, _ := GenerateKey()
	SetKey(k2)
	if _, err := Decrypt(enc); err == nil {
		t.Error("expected decryption with the wrong key to fail")
	}
}

func TestConfigurePrecedenceAndGeneration(t *testing.T) {
	t.Cleanup(func() { SetKey(nil) })

	dir := t.TempDir()

	// Env var wins and is used directly.
	envKey := base64.StdEncoding.EncodeToString(mustKey(t))
	t.Setenv(keyEnv, envKey)
	src, err := Configure("", "", filepath.Join(dir, "unused.key"))
	if err != nil {
		t.Fatalf("Configure(env): %v", err)
	}
	if !strings.Contains(src, keyEnv) {
		t.Errorf("expected env source, got %q", src)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "unused.key")); statErr == nil {
		t.Error("key file should not be generated when the env var is set")
	}
	os.Unsetenv(keyEnv)

	// With nothing set, a key file is generated at the default path, 0600.
	keyPath := filepath.Join(dir, "sub", "encryption.key")
	src, err = Configure("", "", keyPath)
	if err != nil {
		t.Fatalf("Configure(generate): %v", err)
	}
	if !strings.Contains(src, "generated") {
		t.Errorf("expected generated source, got %q", src)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("expected key file created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("key file perms = %o, want 600", perm)
	}

	// A second Configure with the same path loads the existing key (no regen).
	first := currentKey(t)
	src, err = Configure("", "", keyPath)
	if err != nil {
		t.Fatalf("Configure(load): %v", err)
	}
	if strings.Contains(src, "generated") {
		t.Errorf("expected existing key loaded, got %q", src)
	}
	if got := currentKey(t); got != first {
		t.Error("reloading the key file produced a different key")
	}

	// Disabled when nothing is configured and no default path is given.
	SetKey(nil)
	src, err = Configure("", "", "")
	if err != nil {
		t.Fatalf("Configure(disabled): %v", err)
	}
	if Enabled() || !strings.Contains(src, "disabled") {
		t.Errorf("expected disabled, got %q (enabled=%v)", src, Enabled())
	}
}

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

func currentKey(t *testing.T) string {
	t.Helper()
	mu.RLock()
	defer mu.RUnlock()
	return base64.StdEncoding.EncodeToString(key)
}
