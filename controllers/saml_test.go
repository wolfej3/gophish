package controllers

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/logger"
	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/middleware/ratelimit"
	"github.com/gophish/gophish/models"
)

// testSAMLRootURL is the address the test admin server claims to have. The
// test client sends requests for it to the test admin server instead.
const testSAMLRootURL = "https://gophish.test"

// fakeSAMLIdP is a SAML identity provider that signs in whoever is in
// session without asking.
type fakeSAMLIdP struct {
	t      *testing.T
	server *httptest.Server
	idp    *saml.IdentityProvider
	// admin serves Gophish's service provider metadata
	admin *httptest.Server

	mu      sync.Mutex
	session saml.Session
}

func (f *fakeSAMLIdP) GetSession(w http.ResponseWriter, r *http.Request, req *saml.IdpAuthnRequest) *saml.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.session
	s.ID = auth.GenerateSecureKey(8)
	s.CreateTime = time.Now()
	s.ExpireTime = time.Now().Add(time.Hour)
	return &s
}

func (f *fakeSAMLIdP) GetServiceProvider(r *http.Request, serviceProviderID string) (*saml.EntityDescriptor, error) {
	resp, err := f.admin.Client().Get(f.admin.URL + "/saml/metadata")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	sp := &saml.EntityDescriptor{}
	if err := xml.Unmarshal(data, sp); err != nil {
		return nil, err
	}
	if sp.EntityID != serviceProviderID {
		return nil, os.ErrNotExist
	}
	return sp, nil
}

// signIn sets who the identity provider signs in next.
func (f *fakeSAMLIdP) signIn(nameID string, attributes map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.session = saml.Session{NameID: nameID, NameIDFormat: string(saml.EmailAddressNameIDFormat)}
	for name, value := range attributes {
		f.session.CustomAttributes = append(f.session.CustomAttributes, saml.Attribute{
			Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
			Values: []saml.AttributeValue{{Type: "xs:string", Value: value}},
		})
	}
}

// newTestKeyPair returns a self-signed certificate and RSA key.
func newTestKeyPair(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("error generating key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "idp.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("error creating certificate: %v", err)
	}
	cert, _ := x509.ParseCertificate(der)
	return cert, key
}

func newFakeSAMLIdP(t *testing.T) *fakeSAMLIdP {
	f := &fakeSAMLIdP{t: t}
	cert, key := newTestKeyPair(t)
	f.server = httptest.NewUnstartedServer(nil)
	base, _ := url.Parse("http://" + f.server.Listener.Addr().String())
	metadataURL, ssoURL := *base, *base
	metadataURL.Path = "/metadata"
	ssoURL.Path = "/sso"
	f.idp = &saml.IdentityProvider{
		Key:                     key,
		Certificate:             cert,
		Logger:                  logger.DefaultLogger,
		MetadataURL:             metadataURL,
		SSOURL:                  ssoURL,
		ServiceProviderProvider: f,
		SessionProvider:         f,
	}
	f.server.Config.Handler = f.idp.Handler()
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

// setupSAMLTest sets up a test whose admin server signs users in through a
// fake SAML identity provider. The configuration can be adjusted with
// configure.
func setupSAMLTest(t *testing.T, configure func(*config.SAML)) (*testContext, *fakeSAMLIdP) {
	ctx := setupTwoFactorTest(t)
	idp := newFakeSAMLIdP(t)
	dir := t.TempDir()
	conf := &config.SAML{
		IDPMetadataURL: idp.server.URL + "/metadata",
		RootURL:        testSAMLRootURL,
		CertPath:       filepath.Join(dir, "saml.crt"),
		KeyPath:        filepath.Join(dir, "saml.key"),
	}
	if configure != nil {
		configure(conf)
	}
	ctx.config.AdminConf.SAML = conf
	ctx.adminServer.Close()
	server := httptest.NewUnstartedServer(NewAdminServer(ctx.config.AdminConf,
		WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100)))).server.Handler)
	server.StartTLS()
	ctx.adminServer = server
	idp.admin = server
	return ctx, idp
}

// samlForm is the form the identity provider has the browser post to
// Gophish.
type samlForm struct {
	SAMLResponse string
	RelayState   string
}

// samlSignIn starts signing in and has the identity provider sign the user
// in, returning the form it asks the browser to post back to Gophish.
func samlSignIn(t *testing.T, ctx *testContext, client *http.Client, next string) samlForm {
	start := ctx.adminServer.URL + "/login/saml"
	if next != "" {
		start += "?" + url.Values{"next": {next}}.Encode()
	}
	resp, err := client.Get(start)
	if err != nil {
		t.Fatalf("error starting SAML sign in: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect to the identity provider, got %d", resp.StatusCode)
	}
	resp, err = client.Get(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("error requesting the identity provider: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status from the identity provider: %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("error parsing the identity provider's response: %v", err)
	}
	form := samlForm{}
	form.SAMLResponse, _ = doc.Find("input[name='SAMLResponse']").Attr("value")
	form.RelayState, _ = doc.Find("input[name='RelayState']").Attr("value")
	action, _ := doc.Find("form").Attr("action")
	if action != testSAMLRootURL+"/saml/acs" || form.SAMLResponse == "" {
		t.Fatalf("unexpected form from the identity provider: action %q", action)
	}
	return form
}

// postSAMLForm posts the identity provider's form to Gophish, as the browser
// would.
func postSAMLForm(t *testing.T, ctx *testContext, client *http.Client, form samlForm) *http.Response {
	body := url.Values{"SAMLResponse": {form.SAMLResponse}, "RelayState": {form.RelayState}}
	req, _ := http.NewRequest(http.MethodPost, ctx.adminServer.URL+"/saml/acs", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// The browser is coming from the identity provider's site
	req.Header.Set("Origin", "http://idp.test")
	req.Header.Set("Referer", "http://idp.test/sso")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error posting to /saml/acs: %v", err)
	}
	return resp
}

func TestSAMLMetadata(t *testing.T) {
	ctx, _ := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	resp, err := ctx.adminServer.Client().Get(ctx.adminServer.URL + "/saml/metadata")
	if err != nil {
		t.Fatalf("error requesting metadata: %v", err)
	}
	sp := &saml.EntityDescriptor{}
	data, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(data, sp); err != nil {
		t.Fatalf("error parsing metadata: %v", err)
	}
	if sp.EntityID != testSAMLRootURL+"/saml/metadata" {
		t.Fatalf("unexpected entity ID %q", sp.EntityID)
	}
	acs := sp.SPSSODescriptors[0].AssertionConsumerServices[0]
	if acs.Location != testSAMLRootURL+"/saml/acs" || acs.Binding != saml.HTTPPostBinding {
		t.Fatalf("unexpected assertion consumer service %+v", acs)
	}
}

func TestSAMLLinksExistingUser(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	u := createSSOUser(t, "alice@example.com")
	idp.signIn("alice@example.com", nil)
	client := newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "/campaigns")), "/campaigns")
	expectLoggedIn(t, ctx, client, true)
	got, _ := models.GetUser(u.Id)
	if got.SAMLIdentity != idp.server.URL+"/metadata alice@example.com" {
		t.Fatalf("expected the account to be linked, got %q", got.SAMLIdentity)
	}
	logins := auditEntries(t, models.AuditLogin)
	if len(logins) != 1 || logins[0].Details != "single sign-on (saml)" {
		t.Fatalf("unexpected logins: %+v", logins)
	}
}

func TestSAMLUsernameAttribute(t *testing.T) {
	ctx, idp := setupSAMLTest(t, func(c *config.SAML) { c.UsernameAttribute = "email" })
	defer tearDown(t, ctx)
	createSSOUser(t, "bob@example.com")
	idp.signIn("opaque-id-123", map[string]string{"email": "bob@example.com"})
	client := newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "")), "/")
	expectLoggedIn(t, ctx, client, true)
	u, _ := models.GetUserByUsername("bob@example.com")
	if !strings.HasSuffix(u.SAMLIdentity, " opaque-id-123") {
		t.Fatalf("expected the account to be linked to the NameID, got %q", u.SAMLIdentity)
	}
}

func TestSAMLAutoCreatesUser(t *testing.T) {
	ctx, idp := setupSAMLTest(t, func(c *config.SAML) {
		c.AutoCreateUsers = true
		c.AllowedDomains = []string{"example.com"}
	})
	defer tearDown(t, ctx)
	idp.signIn("carol@example.com", nil)
	client := newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "")), "/")
	if _, err := models.GetUserByUsername("carol@example.com"); err != nil {
		t.Fatalf("expected an account to be created: %v", err)
	}
	idp.signIn("eve@elsewhere.com", nil)
	client = newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "")), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSAMLRejectsUnknownUser(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	idp.signIn("mallory@example.com", nil)
	client := newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "")), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSAMLRejectsReplay(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice@example.com")
	idp.signIn("alice@example.com", nil)
	client := newJarClient(t, ctx)
	form := samlSignIn(t, ctx, client, "")
	expectSSORedirect(t, postSAMLForm(t, ctx, client, form), "/")
	// The same response can't be used again, by this browser or another
	expectSSORedirect(t, postSAMLForm(t, ctx, client, form), "/login")
	other := newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, other, form), "/login")
	expectLoggedIn(t, ctx, other, false)
}

func TestSAMLRejectsResponseForAnotherBrowser(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "mallory@example.com")
	idp.signIn("mallory@example.com", nil)
	// An attacker signs in, then has a victim's browser post their response,
	// trying to sign the victim in to the attacker's account
	attacker := newJarClient(t, ctx)
	form := samlSignIn(t, ctx, attacker, "")
	victim := newJarClient(t, ctx)
	samlSignIn(t, ctx, victim, "")
	expectSSORedirect(t, postSAMLForm(t, ctx, victim, form), "/login")
	expectLoggedIn(t, ctx, victim, false)
}

func TestSAMLRejectsWrongRelayState(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice@example.com")
	idp.signIn("alice@example.com", nil)
	client := newJarClient(t, ctx)
	form := samlSignIn(t, ctx, client, "")
	form.RelayState = "forged"
	expectSSORedirect(t, postSAMLForm(t, ctx, client, form), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSAMLRejectsTamperedResponse(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice@example.com")
	idp.signIn("alice@example.com", nil)
	client := newJarClient(t, ctx)
	form := samlSignIn(t, ctx, client, "")
	data, _ := base64.StdEncoding.DecodeString(form.SAMLResponse)
	// The assertion is encrypted to Gophish, so the name isn't readable
	if bytes.Contains(data, []byte("alice@example.com")) {
		t.Fatal("expected the assertion to be encrypted")
	}
	// Change one character of the encrypted assertion
	start := bytes.Index(data, []byte("CipherValue>"))
	if start < 0 {
		t.Fatal("expected an encrypted assertion")
	}
	i := start + len("CipherValue>") + 40
	if data[i] == 'A' {
		data[i] = 'B'
	} else {
		data[i] = 'A'
	}
	form.SAMLResponse = base64.StdEncoding.EncodeToString(data)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, form), "/login")
	expectLoggedIn(t, ctx, client, false)
	if u, _ := models.GetUserByUsername("alice@example.com"); u.SAMLIdentity != "" {
		t.Fatalf("expected alice's account not to be linked, got %q", u.SAMLIdentity)
	}
}

func TestSAMLRejectsUntrustedIdP(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice@example.com")
	idp.signIn("alice@example.com", nil)
	// Gophish loads the real identity provider's metadata on the first
	// sign in
	if resp, err := newJarClient(t, ctx).Get(ctx.adminServer.URL + "/login/saml"); err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("error starting SAML sign in: %v", err)
	}
	// Then someone else's identity provider, with the same address but a
	// different key, signs the response
	cert, key := newTestKeyPair(t)
	idp.idp.Certificate, idp.idp.Key = cert, key
	client := newJarClient(t, ctx)
	expectSSORedirect(t, postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "")), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSAMLRequiresTOTP(t *testing.T) {
	ctx, idp := setupSAMLTest(t, nil)
	defer tearDown(t, ctx)
	u := createSSOUser(t, "alice@example.com")
	secret := auth.GenerateTOTPSecret()
	models.EnableTOTP(&u, secret, 0)
	idp.signIn("alice@example.com", nil)
	client := newJarClient(t, ctx)
	resp := postSAMLForm(t, ctx, client, samlSignIn(t, ctx, client, "/campaigns"))
	expectSSORedirect(t, resp, "/login/2fa")
	expectLoggedIn(t, ctx, client, false)
	loc, _ := resp.Location()
	code, _ := auth.TOTPCode(secret, time.Now())
	expectSSORedirect(t, postForm(t, ctx, client, loc.RequestURI(), url.Values{"code": {code}}), "/campaigns")
	expectLoggedIn(t, ctx, client, true)
}

func TestSSOButtonsForBothProviders(t *testing.T) {
	ctx, _ := setupSAMLTest(t, func(c *config.SAML) { c.ButtonLabel = "Sign in with Okta" })
	defer tearDown(t, ctx)
	// Add OIDC alongside SAML
	idp := newFakeIdP(t)
	ctx.config.AdminConf.OIDC = &config.OIDC{Issuer: idp.server.URL, ClientID: "x", RedirectURL: "https://gophish.test/login/oidc/callback"}
	ctx.adminServer.Close()
	ctx.adminServer = httptest.NewTLSServer(NewAdminServer(ctx.config.AdminConf).server.Handler)
	resp, err := ctx.adminServer.Client().Get(ctx.adminServer.URL + "/login")
	if err != nil {
		t.Fatalf("error requesting /login: %v", err)
	}
	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	labels := []string{}
	doc.Find("a.btn-default").Each(func(i int, s *goquery.Selection) {
		labels = append(labels, strings.TrimSpace(s.Text()))
	})
	if strings.Join(labels, ",") != "Sign in with SSO,Sign in with Okta" {
		t.Fatalf("unexpected single sign-on buttons: %v", labels)
	}
}

func TestSAMLDisabledWithoutConfig(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	for _, path := range []string{"/login/saml", "/saml/metadata"} {
		resp, err := ctx.adminServer.Client().Get(ctx.adminServer.URL + path)
		if err != nil {
			t.Fatalf("error requesting %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected %s to be missing, got %d", path, resp.StatusCode)
		}
	}
}

func TestSAMLConfigValidation(t *testing.T) {
	dir := t.TempDir()
	cases := []config.SAML{
		{RootURL: testSAMLRootURL},
		{IDPMetadataURL: "https://idp.test/metadata"},
		{IDPMetadataURL: "https://idp.test/metadata", RootURL: "gophish.test"},
	}
	for _, c := range cases {
		c.CertPath, c.KeyPath = filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key")
		if _, err := newSAMLClient(c); err == nil {
			t.Fatalf("expected %+v to be rejected", c)
		}
	}
	// A valid configuration creates a key pair, and reuses it after
	c := config.SAML{IDPMetadataURL: "https://idp.test/metadata", RootURL: testSAMLRootURL,
		CertPath: filepath.Join(dir, "s.crt"), KeyPath: filepath.Join(dir, "s.key")}
	first, err := newSAMLClient(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := newSAMLClient(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !first.sp.Certificate.Equal(second.sp.Certificate) {
		t.Fatal("expected the saved key pair to be reused")
	}
	if info, _ := os.Stat(c.KeyPath); info.Mode().Perm() != 0600 {
		t.Fatalf("expected the key to be private, got %v", info.Mode().Perm())
	}
}
