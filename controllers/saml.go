package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/config"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/securecookie"
)

// samlTrackingCookie holds the pending sign in while the user is at the
// identity provider. The identity provider posts its response from its own
// site, and browsers don't send the SameSite=Lax session cookie with such
// requests, so this cookie is SameSite=None and scoped to the ACS path.
const samlTrackingCookie = "gophish_saml"

// samlTrackingMaxAge is how long a user has to sign in at the identity
// provider, in seconds.
const samlTrackingMaxAge = 10 * 60

// samlTracking is the pending sign in stored in the tracking cookie.
type samlTracking struct {
	RequestID  string
	RelayState string
	Next       string
}

// samlClient signs users in to the admin server through a SAML identity
// provider. The provider's metadata is loaded the first time someone signs
// in, so the admin server can start while it's down.
type samlClient struct {
	conf config.SAML
	// tracking signs and encrypts the tracking cookie. Its keys are made
	// when Gophish starts, so a restart abandons sign ins in progress.
	tracking *securecookie.SecureCookie

	mu sync.Mutex
	// sp is the service provider without the identity provider's metadata,
	// which is enough to serve Gophish's own metadata.
	sp saml.ServiceProvider
	// ready is sp with the identity provider's metadata, once it's loaded.
	ready *saml.ServiceProvider
	// loadedAt is when ready's metadata was last loaded. The metadata is
	// refreshed after samlMetadataTTL so an identity provider's signing
	// certificate rotation is picked up without restarting Gophish.
	loadedAt time.Time
}

// samlMetadataTTL is how long loaded identity-provider metadata is used before
// it's refreshed on the next sign in.
const samlMetadataTTL = time.Hour

// newSAMLClient returns a client for the configured identity provider, with
// defaults applied, or an error if the configuration is incomplete.
func newSAMLClient(conf config.SAML) (*samlClient, error) {
	if conf.IDPMetadataURL == "" && conf.IDPMetadataPath == "" {
		return nil, errors.New("saml: idp_metadata_url or idp_metadata_path is required")
	}
	root, err := url.Parse(strings.TrimRight(conf.RootURL, "/"))
	if err != nil || root.Scheme == "" || root.Host == "" {
		return nil, errors.New("saml: root_url must be the admin server's address, such as https://gophish.example.com:3333")
	}
	if conf.CertPath == "" {
		conf.CertPath = "gophish_saml.crt"
	}
	if conf.KeyPath == "" {
		conf.KeyPath = "gophish_saml.key"
	}
	if conf.ButtonLabel == "" {
		conf.ButtonLabel = "Sign in with SAML"
	}
	if conf.DefaultRole == "" {
		conf.DefaultRole = models.RoleUser
	}
	if conf.AutoCreateUsers {
		if conf.DefaultRole == models.RoleAdmin && !conf.AllowAdminAutoCreate {
			return nil, errors.New("auto_create_users with default_role \"admin\" is refused; set allow_admin_auto_create to confirm")
		}
		if len(conf.AllowedDomains) == 0 {
			log.Warn("single sign-on auto_create_users is on with no allowed_domains; anyone who can authenticate at the identity provider gets an account")
		}
	}
	cert, key, err := loadSAMLKeyPair(conf.CertPath, conf.KeyPath, root.Hostname())
	if err != nil {
		return nil, fmt.Errorf("saml: %w", err)
	}
	metadataURL := *root
	metadataURL.Path += "/saml/metadata"
	acsURL := *root
	acsURL.Path += "/saml/acs"
	entityID := conf.EntityID
	if entityID == "" {
		entityID = metadataURL.String()
	}
	tracking := securecookie.New(securecookie.GenerateRandomKey(32), securecookie.GenerateRandomKey(32))
	tracking.MaxAge(samlTrackingMaxAge)
	return &samlClient{
		conf:     conf,
		tracking: tracking,
		sp: saml.ServiceProvider{
			EntityID:    entityID,
			Key:         key,
			Certificate: cert,
			MetadataURL: metadataURL,
			AcsURL:      acsURL,
			HTTPClient:  &http.Client{Timeout: oidcTimeout},
			// Only accept responses to sign ins started here, so that an
			// attacker can't sign a victim in to the attacker's account.
			AllowIDPInitiated: false,
		},
	}, nil
}

// loadSAMLKeyPair loads the certificate and RSA key Gophish signs SAML
// requests with, creating a self-signed pair if they don't exist.
func loadSAMLKeyPair(certPath, keyPath, host string) (*x509.Certificate, *rsa.PrivateKey, error) {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	if os.IsNotExist(certErr) && os.IsNotExist(keyErr) {
		log.Infof("Creating a new self-signed certificate for SAML in %s", certPath)
		if err := createSAMLKeyPair(certPath, keyPath, host); err != nil {
			return nil, nil, err
		}
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, nil, err
	}
	key, ok := pair.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("%s must hold an RSA key", keyPath)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	return cert, key, err
}

// createSAMLKeyPair writes a new self-signed certificate and RSA key.
func createSAMLKeyPair(certPath, keyPath, host string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Gophish"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return err
	}
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer certFile.Close()
	return pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// serviceProvider returns the service provider, loading the identity
// provider's metadata if we haven't yet, or refreshing it once it's older than
// samlMetadataTTL. A transient refresh failure keeps the cached copy so sign in
// still works.
func (c *samlClient) serviceProvider(ctx context.Context) (*saml.ServiceProvider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready != nil && time.Since(c.loadedAt) < samlMetadataTTL {
		return c.ready, nil
	}
	sp, err := c.loadMetadataLocked(ctx)
	if err != nil {
		if c.ready != nil {
			log.Warnf("saml: refreshing identity provider metadata failed, using cached copy: %v", err)
			return c.ready, nil
		}
		return nil, err
	}
	return sp, nil
}

// loadMetadataLocked fetches and parses the identity provider's metadata and
// updates the cache. The caller must hold c.mu.
func (c *samlClient) loadMetadataLocked(ctx context.Context) (*saml.ServiceProvider, error) {
	var data []byte
	var err error
	if c.conf.IDPMetadataPath != "" {
		data, err = os.ReadFile(c.conf.IDPMetadataPath)
	} else {
		data, err = fetchSAMLMetadata(ctx, c.sp.HTTPClient, c.conf.IDPMetadataURL)
	}
	var metadata *saml.EntityDescriptor
	if err == nil {
		metadata, err = parseIDPMetadata(data)
	}
	if err != nil {
		return nil, fmt.Errorf("loading identity provider metadata: %w", err)
	}
	sp := c.sp
	sp.IDPMetadata = metadata
	c.ready = &sp
	c.loadedAt = time.Now()
	return c.ready, nil
}

// maxSAMLMetadataSize limits how much metadata we read from the identity
// provider.
const maxSAMLMetadataSize = 10 << 20

// fetchSAMLMetadata downloads the identity provider's metadata.
func fetchSAMLMetadata(ctx context.Context, client *http.Client, metadataURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", metadataURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSAMLMetadataSize))
}

// parseIDPMetadata parses identity provider metadata. Metadata holding
// several entities uses the first one that's an identity provider.
func parseIDPMetadata(data []byte) (*saml.EntityDescriptor, error) {
	entity := &saml.EntityDescriptor{}
	if err := xml.Unmarshal(data, entity); err == nil && len(entity.IDPSSODescriptors) > 0 {
		return entity, nil
	}
	entities := &saml.EntitiesDescriptor{}
	if err := xml.Unmarshal(data, entities); err != nil {
		return nil, err
	}
	for i := range entities.EntityDescriptors {
		if len(entities.EntityDescriptors[i].IDPSSODescriptors) > 0 {
			return &entities.EntityDescriptors[i], nil
		}
	}
	return nil, errors.New("metadata doesn't describe an identity provider")
}

// accounts returns how the configuration matches and creates accounts.
func (c *samlClient) accounts() ssoAccounts {
	return ssoAccounts{
		AutoCreateUsers: c.conf.AutoCreateUsers,
		DefaultRole:     c.conf.DefaultRole,
		AllowedDomains:  c.conf.AllowedDomains,
	}
}

// SAMLMetadata serves Gophish's service provider metadata, for setting up
// the identity provider.
func (as *AdminServer) SAMLMetadata(w http.ResponseWriter, r *http.Request) {
	if as.saml == nil {
		http.NotFound(w, r)
		return
	}
	data, err := xml.MarshalIndent(as.saml.sp.Metadata(), "", "  ")
	if err != nil {
		log.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	w.Write(data)
}

// SAMLLogin sends the user to the identity provider to sign in.
func (as *AdminServer) SAMLLogin(w http.ResponseWriter, r *http.Request) {
	if as.saml == nil {
		http.NotFound(w, r)
		return
	}
	reqCtx, cancel := context.WithTimeout(r.Context(), oidcTimeout)
	defer cancel()
	sp, err := as.saml.serviceProvider(reqCtx)
	if err != nil {
		as.ssoError(w, r, err)
		return
	}
	req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		as.ssoError(w, r, fmt.Errorf("making authentication request: %w", err))
		return
	}
	tracking := samlTracking{
		RequestID:  req.ID,
		RelayState: auth.GenerateSecureKey(16),
		Next:       safeNext(r.FormValue("next")),
	}
	redirect, err := req.Redirect(tracking.RelayState, sp)
	if err != nil {
		as.ssoError(w, r, fmt.Errorf("making authentication request: %w", err))
		return
	}
	value, err := as.saml.tracking.Encode(samlTrackingCookie, tracking)
	if err != nil {
		as.ssoError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     samlTrackingCookie,
		Value:    value,
		Path:     "/saml/acs",
		MaxAge:   samlTrackingMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// SAMLACS handles the identity provider posting the user's signed assertion
// back to Gophish, and logs them in to the matching Gophish account.
func (as *AdminServer) SAMLACS(w http.ResponseWriter, r *http.Request) {
	if as.saml == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	// Each sign in attempt can only be completed once
	http.SetCookie(w, &http.Cookie{Name: samlTrackingCookie, Path: "/saml/acs", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteNoneMode})
	tracking := samlTracking{}
	cookie, err := r.Cookie(samlTrackingCookie)
	if err == nil {
		err = as.saml.tracking.Decode(samlTrackingCookie, cookie.Value, &tracking)
	}
	if err != nil {
		as.ssoError(w, r, fmt.Errorf("no SAML sign in in progress: %w", err))
		return
	}
	if subtle.ConstantTimeCompare([]byte(tracking.RelayState), []byte(r.FormValue("RelayState"))) != 1 {
		as.ssoError(w, r, errors.New("RelayState doesn't match"))
		return
	}
	reqCtx, cancel := context.WithTimeout(r.Context(), oidcTimeout)
	defer cancel()
	sp, err := as.saml.serviceProvider(reqCtx)
	if err != nil {
		as.ssoError(w, r, err)
		return
	}
	assertion, err := sp.ParseResponse(r, []string{tracking.RequestID})
	if err != nil {
		var invalid *saml.InvalidResponseError
		if errors.As(err, &invalid) {
			err = invalid.PrivateErr
		}
		as.ssoError(w, r, fmt.Errorf("invalid SAML response: %w", err))
		return
	}
	if assertion.Subject == nil || assertion.Subject.NameID == nil || assertion.Subject.NameID.Value == "" {
		as.ssoError(w, r, errors.New("assertion has no NameID"))
		return
	}
	nameID := assertion.Subject.NameID.Value
	username := nameID
	if attr := as.saml.conf.UsernameAttribute; attr != "" {
		username = samlAttribute(assertion, attr)
		if username == "" {
			as.ssoError(w, r, fmt.Errorf("assertion has no %q attribute", attr))
			return
		}
	}
	as.finishSSOLogin(w, r, ssoIdentity{
		Provider: models.SSOProviderSAML,
		Identity: assertion.Issuer.Value + " " + nameID,
		Username: username,
	}, as.saml.accounts(), tracking.Next)
}

// samlAttribute returns the first value of the assertion attribute with the
// given name or friendly name.
func samlAttribute(assertion *saml.Assertion, name string) string {
	for _, statement := range assertion.AttributeStatements {
		for _, attr := range statement.Attributes {
			if (attr.Name == name || attr.FriendlyName == name) && len(attr.Values) > 0 {
				return attr.Values[0].Value
			}
		}
	}
	return ""
}
