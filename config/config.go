package config

import (
	"encoding/json"
	"io/ioutil"

	log "github.com/gophish/gophish/logger"
)

// AdminServer represents the Admin server configuration details
type AdminServer struct {
	ListenURL            string   `json:"listen_url"`
	UseTLS               bool     `json:"use_tls"`
	CertPath             string   `json:"cert_path"`
	KeyPath              string   `json:"key_path"`
	CSRFKey              string   `json:"csrf_key"`
	AllowedInternalHosts []string `json:"allowed_internal_hosts"`
	TrustedOrigins       []string `json:"trusted_origins"`
	OIDC                 *OIDC    `json:"oidc"`
	SAML                 *SAML    `json:"saml"`
	// Directory lists connectors that groups can sync their members from.
	Directory []DirectoryConnector `json:"directory"`
}

// DirectoryConnector holds the credentials for syncing group members from an
// identity directory. Secrets live here, in config.json, rather than in the
// database; groups reference a connector by name.
type DirectoryConnector struct {
	// Name identifies the connector to the admin interface and to groups
	// that sync from it.
	Name string `json:"name"`
	// Provider is "msgraph" (Microsoft Entra ID), "google" (Google
	// Workspace), or "ldap" (any LDAP directory, including Active
	// Directory).
	Provider string `json:"provider"`

	// Microsoft Entra ID (msgraph)
	TenantID     string `json:"tenant_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`

	// Google Workspace (google). CredentialsFile is a service account key
	// with domain-wide delegation; DelegatedSubject is the admin whose
	// access it borrows.
	CredentialsFile  string `json:"credentials_file"`
	DelegatedSubject string `json:"delegated_subject"`

	// LDAP / Active Directory (ldap). URL is the server, e.g.
	// ldaps://dc.example.com:636. BindDN and BindPassword authenticate the
	// account Gophish searches with. BaseDN is where searches start.
	URL          string `json:"url"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"bind_password"`
	BaseDN       string `json:"base_dn"`
	// UserFilter finds the members of a directory group. %s is replaced with
	// the group named by a source's external_id. It defaults to the
	// Active Directory filter
	// (&(objectClass=user)(memberOf=%s)), so external_id is the group's DN.
	UserFilter string `json:"user_filter"`
	// The attributes read from each user. They default to the Active
	// Directory names: mail, givenName, sn.
	EmailAttribute     string `json:"email_attribute"`
	FirstNameAttribute string `json:"first_name_attribute"`
	LastNameAttribute  string `json:"last_name_attribute"`
	// StartTLS upgrades a plain ldap:// connection to TLS. InsecureTLS skips
	// certificate verification (for self-signed lab certs; avoid in
	// production).
	StartTLS    bool `json:"start_tls"`
	InsecureTLS bool `json:"insecure_tls"`

	// BaseURL overrides the directory API's base address, for self-hosted
	// endpoints or testing. Empty uses the provider's public endpoint.
	BaseURL string `json:"base_url"`
	// TokenURL overrides the OAuth token endpoint, for the same reasons.
	TokenURL string `json:"token_url"`
}

// SAML configures single sign-on to the admin server through a SAML 2.0
// identity provider.
type SAML struct {
	// IDPMetadataURL or IDPMetadataPath locate the identity provider's
	// metadata. One of them is required.
	IDPMetadataURL  string `json:"idp_metadata_url"`
	IDPMetadataPath string `json:"idp_metadata_path"`
	// RootURL is the address browsers use to reach the admin server, such as
	// https://gophish.example.com:3333. The identity provider sends users
	// back to RootURL/saml/acs, and reads Gophish's metadata from
	// RootURL/saml/metadata.
	RootURL string `json:"root_url"`
	// EntityID identifies Gophish to the identity provider. Defaults to the
	// metadata URL.
	EntityID string `json:"entity_id"`
	// CertPath and KeyPath hold the certificate and RSA key Gophish signs
	// requests with. They're created if they don't exist. Default to
	// gophish_saml.crt and gophish_saml.key.
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
	// UsernameAttribute is the assertion attribute matched against Gophish
	// usernames. Defaults to the assertion's NameID.
	UsernameAttribute string `json:"username_attribute"`
	// ButtonLabel is the text of the sign in button on the login page.
	// Defaults to "Sign in with SAML".
	ButtonLabel string `json:"button_label"`
	// AutoCreateUsers, DefaultRole, AllowedDomains, and DisablePasswordLogin
	// work as they do for OIDC.
	AutoCreateUsers      bool     `json:"auto_create_users"`
	DefaultRole          string   `json:"default_role"`
	AllowedDomains       []string `json:"allowed_domains"`
	DisablePasswordLogin bool     `json:"disable_password_login"`
}

// OIDC configures single sign-on to the admin server through an OpenID
// Connect identity provider.
type OIDC struct {
	// Issuer is the identity provider's issuer URL, from which its
	// configuration is discovered.
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// RedirectURL is the admin server's callback URL registered with the
	// identity provider, ending in /login/oidc/callback.
	RedirectURL string `json:"redirect_url"`
	// Scopes requested in addition to "openid". Defaults to "email" and
	// "profile".
	Scopes []string `json:"scopes"`
	// UsernameClaim is the ID token claim matched against Gophish usernames.
	// Defaults to "email".
	UsernameClaim string `json:"username_claim"`
	// ButtonLabel is the text of the sign in button on the login page.
	// Defaults to "Sign in with SSO".
	ButtonLabel string `json:"button_label"`
	// AutoCreateUsers creates a Gophish account for people who sign in
	// without one, with the role in DefaultRole.
	AutoCreateUsers bool   `json:"auto_create_users"`
	DefaultRole     string `json:"default_role"`
	// AllowedDomains, if set, limits sign in to usernames ending in one of
	// these email domains.
	AllowedDomains []string `json:"allowed_domains"`
	// DisablePasswordLogin turns off signing in with a Gophish password, so
	// that everyone signs in through the identity provider. API keys still
	// work. To recover if the identity provider is unavailable, remove this
	// setting and restart Gophish.
	DisablePasswordLogin bool `json:"disable_password_login"`
}

// PhishServer represents the Phish server configuration details
type PhishServer struct {
	ListenURL string `json:"listen_url"`
	UseTLS    bool   `json:"use_tls"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
}

// Config represents the configuration information.
type Config struct {
	AdminConf      AdminServer `json:"admin_server"`
	PhishConf      PhishServer `json:"phish_server"`
	DBName         string      `json:"db_name"`
	DBPath         string      `json:"db_path"`
	DBSSLCaPath    string      `json:"db_sslca_path"`
	TestFlag       bool        `json:"test_flag"`
	ContactAddress string      `json:"contact_address"`
	Logging        *log.Config `json:"logging"`
}

// Version contains the current gophish version
var Version = ""

// ServerName is the server type that is returned in the transparency response.
const ServerName = "gophish"

// LoadConfig loads the configuration from the specified filepath
func LoadConfig(configPath string) (*Config, error) {
	// Get the config file
	configFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	err = json.Unmarshal(configFile, config)
	if err != nil {
		return nil, err
	}
	if config.Logging == nil {
		config.Logging = &log.Config{}
	}
	// Explicitly set the TestFlag to false to prevent config.json overrides
	config.TestFlag = false
	return config, nil
}
