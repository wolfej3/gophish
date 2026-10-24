package sender

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeServiceAccount writes a service account key whose token_uri points at
// tokenURL, and returns its path.
func writeServiceAccount(t *testing.T, tokenURL string) string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("error generating key: %v", err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	sa := map[string]string{
		"type": "service_account", "project_id": "test", "private_key": pemKey,
		"client_email": "svc@test.iam.gserviceaccount.com", "token_uri": tokenURL,
	}
	data, _ := json.Marshal(sa)
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("error writing service account: %v", err)
	}
	return path
}

func TestGmailSend(t *testing.T) {
	var gotRaw string
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/users/me/messages/send", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body := map[string]string{}
		json.NewDecoder(r.Body).Decode(&body)
		gotRaw = body["raw"]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dialer, err := NewGmailDialer(GmailConfig{
		CredentialsFile: writeServiceAccount(t, server.URL+"/token"),
		From:            "sender@example.com", BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sender, _ := dialer.Dial()
	if err := sender.Send("sender@example.com", []string{"t@example.com"}, testMessage(t)); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	mime, err := base64.URLEncoding.DecodeString(gotRaw)
	if err != nil {
		t.Fatalf("raw wasn't base64url MIME: %v", err)
	}
	if !strings.Contains(string(mime), "Subject: Hello") {
		t.Fatalf("MIME didn't contain the message: %s", mime)
	}
}

func TestGmailConfigValidation(t *testing.T) {
	if _, err := NewGmailDialer(GmailConfig{From: "a@b.com"}); err == nil {
		t.Fatal("expected missing credentials to be rejected")
	}
	if _, err := NewGmailDialer(GmailConfig{CredentialsFile: "/nonexistent"}); err == nil {
		t.Fatal("expected missing from to be rejected")
	}
	if _, err := NewGmailDialer(GmailConfig{CredentialsFile: "/nonexistent", From: "a@b.com"}); err == nil {
		t.Fatal("expected unreadable credentials to be rejected")
	}
}

// ensure Reset/Close are no-ops and present
func TestGmailSenderCloseReset(t *testing.T) {
	s := &gmailSender{}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	_ = io.Discard
}
