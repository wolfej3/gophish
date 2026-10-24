package controllers

import (
	"path/filepath"
	"testing"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
)

func TestOIDCRefusesAutoCreateAdmin(t *testing.T) {
	base := config.OIDC{Issuer: "https://login.example.com", ClientID: "c", RedirectURL: "https://g.example.com/login/oidc/callback",
		AutoCreateUsers: true, DefaultRole: models.RoleAdmin}
	if _, err := newOIDCClient(base); err == nil {
		t.Fatal("expected auto-create admin to be refused without allow_admin_auto_create")
	}
	base.AllowAdminAutoCreate = true
	if _, err := newOIDCClient(base); err != nil {
		t.Fatalf("expected the explicit flag to allow it, got %v", err)
	}
}

func TestSAMLRefusesAutoCreateAdmin(t *testing.T) {
	dir := t.TempDir()
	base := config.SAML{IDPMetadataURL: "https://idp.example.com/metadata", RootURL: "https://g.example.com",
		CertPath: filepath.Join(dir, "c.crt"), KeyPath: filepath.Join(dir, "c.key"),
		AutoCreateUsers: true, DefaultRole: models.RoleAdmin}
	if _, err := newSAMLClient(base); err == nil {
		t.Fatal("expected auto-create admin to be refused without allow_admin_auto_create")
	}
	base.AllowAdminAutoCreate = true
	if _, err := newSAMLClient(base); err != nil {
		t.Fatalf("expected the explicit flag to allow it, got %v", err)
	}
}
