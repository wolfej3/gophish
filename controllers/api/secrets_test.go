package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

// TestSMTPSecretsNotReturned ensures sending-profile secrets are never sent in
// API responses and that a placeholder on PUT keeps the stored value.
func TestSMTPSecretsNotReturned(t *testing.T) {
	testCtx := setupTest(t)
	profile := models.SMTP{
		Name: "Prof", Interface: models.SMTPInterface, FromAddress: "a@b.com",
		Host: "smtp.example.com:25", Username: "user", Password: "sup3rsecret",
	}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/smtp/", testCtx.apiKey, profile)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	created := models.SMTP{}
	json.NewDecoder(w.Body).Decode(&created)
	if created.Password == "sup3rsecret" {
		t.Fatal("POST response leaked the SMTP password")
	}

	// GET must not return the real password.
	w = apiRequest(t, testCtx, http.MethodGet, fmt.Sprintf("/api/smtp/%d", created.Id), testCtx.apiKey, nil)
	got := models.SMTP{}
	json.NewDecoder(w.Body).Decode(&got)
	if got.Password == "sup3rsecret" {
		t.Fatal("GET leaked the SMTP password")
	}
	if got.Password != secretPlaceholder {
		t.Fatalf("expected placeholder, got %q", got.Password)
	}

	// PUT with the placeholder keeps the stored password.
	got.Host = "smtp.example.com:587"
	w = apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/smtp/%d", created.Id), testCtx.apiKey, got)
	if w.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", w.Code, w.Body.String())
	}
	stored, _ := models.GetSMTP(created.Id, 1)
	if stored.Password != "sup3rsecret" {
		t.Fatalf("placeholder PUT changed the stored password to %q", stored.Password)
	}
	if stored.Host != "smtp.example.com:587" {
		t.Fatal("the host update wasn't saved")
	}

	// PUT with a real new password updates it.
	got.Password = "newsecret"
	apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/smtp/%d", created.Id), testCtx.apiKey, got)
	stored, _ = models.GetSMTP(created.Id, 1)
	if stored.Password != "newsecret" {
		t.Fatalf("expected the password to update, got %q", stored.Password)
	}
}

// TestGraphClientSecretNotReturned ensures the Graph client secret is redacted.
func TestGraphClientSecretNotReturned(t *testing.T) {
	testCtx := setupTest(t)
	profile := models.SMTP{
		Name: "Graph", Interface: models.GraphInterface, FromAddress: "a@b.com",
		TenantID: "t", ClientID: "c", ClientSecret: "graphsecret",
	}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/smtp/", testCtx.apiKey, profile)
	created := models.SMTP{}
	json.NewDecoder(w.Body).Decode(&created)
	w = apiRequest(t, testCtx, http.MethodGet, fmt.Sprintf("/api/smtp/%d", created.Id), testCtx.apiKey, nil)
	got := models.SMTP{}
	json.NewDecoder(w.Body).Decode(&got)
	if got.ClientSecret == "graphsecret" {
		t.Fatal("GET leaked the Graph client secret")
	}
	// Editing without changing the secret keeps it.
	got.TenantID = "t2"
	apiRequest(t, testCtx, http.MethodPut, fmt.Sprintf("/api/smtp/%d", created.Id), testCtx.apiKey, got)
	stored, _ := models.GetSMTP(created.Id, 1)
	if stored.ClientSecret != "graphsecret" {
		t.Fatalf("placeholder PUT changed the client secret to %q", stored.ClientSecret)
	}
}
