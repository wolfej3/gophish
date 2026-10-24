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
