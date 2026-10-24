package directory

import (
	"testing"

	"github.com/gophish/gophish/config"
)

func TestLDAPRequiresConfig(t *testing.T) {
	// Missing URL
	if _, err := newProvider(config.DirectoryConnector{Provider: "ldap", BaseDN: "dc=x"}); err == nil {
		t.Fatal("expected missing url to be rejected")
	}
	// Missing base DN
	if _, err := newProvider(config.DirectoryConnector{Provider: "ldap", URL: "ldaps://x"}); err == nil {
		t.Fatal("expected missing base_dn to be rejected")
	}
	// Valid
	if _, err := newProvider(config.DirectoryConnector{Provider: "ldap", URL: "ldaps://dc.example.com", BaseDN: "dc=example,dc=com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnknownProvider(t *testing.T) {
	if _, err := newProvider(config.DirectoryConnector{Provider: "carrier-pigeon"}); err == nil {
		t.Fatal("expected an unknown provider to be rejected")
	}
}

func TestAttrOr(t *testing.T) {
	if attrOr("", "mail") != "mail" {
		t.Fatal("expected the fallback when empty")
	}
	if attrOr("userPrincipalName", "mail") != "userPrincipalName" {
		t.Fatal("expected the provided value")
	}
}

func TestLDAPRefusesPlaintextWithoutTLS(t *testing.T) {
	base := config.DirectoryConnector{Provider: "ldap", BaseDN: "dc=example,dc=com"}
	// Plain ldap:// with no start_tls/insecure is refused.
	c := base
	c.URL = "ldap://dc.example.com"
	if _, err := newProvider(c); err == nil {
		t.Fatal("expected plaintext ldap:// to be refused")
	}
	// ldaps:// is fine.
	c.URL = "ldaps://dc.example.com"
	if _, err := newProvider(c); err != nil {
		t.Fatalf("expected ldaps:// to be allowed, got %v", err)
	}
	// ldap:// with start_tls is fine.
	c.URL, c.StartTLS = "ldap://dc.example.com", true
	if _, err := newProvider(c); err != nil {
		t.Fatalf("expected ldap:// + start_tls to be allowed, got %v", err)
	}
}
