package sender

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophish/gomail"
)

// testMessage returns a simple message implementing io.WriterTo.
func testMessage(t *testing.T) io.WriterTo {
	m := gomail.NewMessage()
	m.SetHeader("From", "sender@example.com")
	m.SetHeader("To", "target@example.com")
	m.SetHeader("Subject", "Hello")
	m.SetBody("text/plain", "This is the body")
	return m
}

func TestGraphSend(t *testing.T) {
	var gotBody string
	var gotAuth string
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dialer, err := NewGraphDialer(GraphConfig{
		TenantID: "t", ClientID: "c", ClientSecret: "s", From: "sender@example.com",
		BaseURL: server.URL, TokenURL: server.URL + "/token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sender, _ := dialer.Dial()
	if err := sender.Send("sender@example.com", []string{"target@example.com"}, testMessage(t)); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	if gotPath != "/users/sender@example.com/sendMail" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	mime, err := base64.StdEncoding.DecodeString(gotBody)
	if err != nil {
		t.Fatalf("body wasn't base64 MIME: %v", err)
	}
	if !strings.Contains(string(mime), "Subject: Hello") || !strings.Contains(string(mime), "This is the body") {
		t.Fatalf("MIME didn't contain the message: %s", mime)
	}
}

func TestGraphSendReportsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"forbidden"}}`, http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	dialer, _ := NewGraphDialer(GraphConfig{TenantID: "t", ClientID: "c", ClientSecret: "s", From: "a@b.com", BaseURL: server.URL, TokenURL: server.URL + "/token"})
	sender, _ := dialer.Dial()
	err := sender.Send("a@b.com", []string{"x@y.com"}, testMessage(t))
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}

func TestGraphConfigValidation(t *testing.T) {
	cases := []GraphConfig{
		{ClientID: "c", ClientSecret: "s", From: "a@b.com"},
		{TenantID: "t", ClientSecret: "s", From: "a@b.com"},
		{TenantID: "t", ClientID: "c", From: "a@b.com"},
		{TenantID: "t", ClientID: "c", ClientSecret: "s"},
	}
	for _, c := range cases {
		if _, err := NewGraphDialer(c); err == nil {
			t.Fatalf("expected %+v to be rejected", c)
		}
	}
}
