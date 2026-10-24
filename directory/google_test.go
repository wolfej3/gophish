package directory

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gophish/gophish/config"
)

// writeServiceAccount writes a service account key whose token_uri points at
// tokenURL, and returns its path.
func writeServiceAccount(t *testing.T, tokenURL string) string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("error generating key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
	sa := map[string]string{
		"type":         "service_account",
		"project_id":   "test",
		"private_key":  pemKey,
		"client_email": "svc@test.iam.gserviceaccount.com",
		"token_uri":    tokenURL,
	}
	data, _ := json.Marshal(sa)
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("error writing service account: %v", err)
	}
	return path
}

func newGoogleServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		// members list vs users get are both under different paths; this
		// handler serves /groups/{id}/members
		json.NewEncoder(w).Encode(map[string]interface{}{
			"members": []map[string]string{
				{"email": "alice@example.com", "type": "USER"},
				{"email": "group@example.com", "type": "GROUP"}, // dropped
			},
		})
	})
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"primaryEmail": "alice@example.com",
			"name":         map[string]string{"givenName": "Alice", "familyName": "Adams"},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestGoogleMembers(t *testing.T) {
	server := newGoogleServer(t)
	conn := config.DirectoryConnector{
		Provider:         "google",
		CredentialsFile:  writeServiceAccount(t, server.URL+"/token"),
		DelegatedSubject: "admin@example.com",
		BaseURL:          server.URL,
	}
	members, err := FetchMembers(context.Background(), conn, "group@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 user member (group dropped), got %d: %+v", len(members), members)
	}
	if members[0].Email != "alice@example.com" || members[0].FirstName != "Alice" || members[0].LastName != "Adams" {
		t.Fatalf("unexpected member: %+v", members[0])
	}
}

func TestGoogleRequiresConfig(t *testing.T) {
	if _, err := newProvider(config.DirectoryConnector{Provider: "google"}); err == nil {
		t.Fatal("expected missing config to be rejected")
	}
}
