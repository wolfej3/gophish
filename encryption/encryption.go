// Package encryption provides authenticated encryption for secrets stored at
// rest — sending-profile and IMAP passwords and the Microsoft Graph client
// secret — as defense in depth against disclosure of the database file or a
// database backup.
//
// Secrets are encrypted with AES-256-GCM. An encrypted value is stored as the
// string "enc:v1:" followed by the URL-safe base64 of the random 12-byte
// nonce concatenated with the GCM ciphertext. A value without that prefix is
// treated as plaintext, so a database written before encryption was enabled
// keeps working and is upgraded in place on first run.
//
// The AES key is process-global and set once at startup via Configure. When
// no key is configured (for example in unit tests), encryption is disabled:
// Encrypt returns its input unchanged and Decrypt passes plaintext through,
// so callers need no special-casing.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// prefix marks a value as encrypted with version 1 of the scheme.
const prefix = "enc:v1:"

// keyEnv is the environment variable that may hold a base64-encoded AES key.
const keyEnv = "GOPHISH_ENCRYPTION_KEY"

var (
	mu  sync.RWMutex
	key []byte
)

// ErrNoKey is returned when an encrypted value must be decrypted but no key
// is configured.
var ErrNoKey = errors.New("encryption: an encrypted value was read but no encryption key is configured")

// IsEncrypted reports whether a stored value is in the encrypted form.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, prefix)
}

// Enabled reports whether an encryption key is configured.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return key != nil
}

// SetKey sets the process-global AES key. The key must be 16, 24, or 32 bytes
// (AES-128/192/256). A nil key disables encryption. It is used directly by
// tests; normal startup goes through Configure.
func SetKey(k []byte) error {
	if k != nil {
		switch len(k) {
		case 16, 24, 32:
		default:
			return fmt.Errorf("encryption: key must be 16, 24, or 32 bytes, got %d", len(k))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	key = k
	return nil
}

// GenerateKey returns a new random 32-byte (AES-256) key.
func GenerateKey() ([]byte, error) {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// Configure resolves the AES key and enables encryption, returning a short
// human-readable description of where the key came from (for logging). The
// order of precedence is:
//
//  1. the GOPHISH_ENCRYPTION_KEY environment variable (base64)
//  2. inlineKey, a base64 key from config.json
//  3. keyFile, a path to a base64 key file
//  4. defaultKeyPath: a new key is generated and written there with 0600
//     permissions
//
// If none of these is set and defaultKeyPath is empty, encryption stays
// disabled and the source is reported as "disabled" — this is the path tests
// take, so they are unaffected.
func Configure(inlineKey, keyFile, defaultKeyPath string) (string, error) {
	if env := os.Getenv(keyEnv); env != "" {
		k, err := decodeKey(env)
		if err != nil {
			return "", fmt.Errorf("%s: %w", keyEnv, err)
		}
		return keyEnv + " environment variable", SetKey(k)
	}
	if inlineKey != "" {
		k, err := decodeKey(inlineKey)
		if err != nil {
			return "", fmt.Errorf("encryption_key in config: %w", err)
		}
		return "encryption_key in config", SetKey(k)
	}
	if keyFile != "" {
		k, err := readKeyFile(keyFile)
		if err != nil {
			return "", err
		}
		return "key file " + keyFile, SetKey(k)
	}
	if defaultKeyPath != "" {
		k, generated, err := loadOrCreateKeyFile(defaultKeyPath)
		if err != nil {
			return "", err
		}
		if generated {
			return "generated new key file " + defaultKeyPath, SetKey(k)
		}
		return "key file " + defaultKeyPath, SetKey(k)
	}
	return "disabled (no key configured)", nil
}

// decodeKey decodes a base64 key (standard or URL encoding, with or without
// padding) and validates its length.
func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if k, err := enc.DecodeString(s); err == nil {
			switch len(k) {
			case 16, 24, 32:
				return k, nil
			}
		}
	}
	return nil, errors.New("not a valid base64-encoded 16/24/32-byte key")
}

// readKeyFile reads and decodes a base64 key from the given path.
func readKeyFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading encryption key file: %w", err)
	}
	k, err := decodeKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("encryption key file %s: %w", path, err)
	}
	return k, nil
}

// loadOrCreateKeyFile reads the base64 key at path, or, if the file does not
// exist, generates a new key and writes it there with 0600 permissions.
func loadOrCreateKeyFile(path string) (k []byte, generated bool, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		k, err = readKeyFile(path)
		return k, false, err
	} else if !os.IsNotExist(statErr) {
		return nil, false, statErr
	}
	k, err = GenerateKey()
	if err != nil {
		return nil, false, err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, false, err
		}
	}
	encoded := base64.StdEncoding.EncodeToString(k)
	if err := os.WriteFile(path, []byte(encoded+"\n"), 0600); err != nil {
		return nil, false, fmt.Errorf("writing encryption key file: %w", err)
	}
	return k, true, nil
}

// Encrypt returns the encrypted, prefixed form of plaintext. An empty string
// and an already-encrypted value are returned unchanged, and when no key is
// configured the plaintext is returned unchanged (encryption disabled).
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" || IsEncrypted(plaintext) {
		return plaintext, nil
	}
	mu.RLock()
	k := key
	mu.RUnlock()
	if k == nil {
		return plaintext, nil
	}
	gcm, err := newGCM(k)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt returns the plaintext for a value produced by Encrypt. A value
// without the encrypted prefix is returned unchanged, so plaintext written
// before encryption was enabled is read transparently.
func Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}
	mu.RLock()
	k := key
	mu.RUnlock()
	if k == nil {
		return "", ErrNoKey
	}
	gcm, err := newGCM(k)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", fmt.Errorf("encryption: malformed ciphertext: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("encryption: ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("encryption: decryption failed (wrong key?): %w", err)
	}
	return string(plaintext), nil
}

func newGCM(k []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
