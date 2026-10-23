package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-jose/go-jose/v4"
	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/middleware/ratelimit"
	"github.com/gophish/gophish/models"
)

const (
	testOIDCClientID     = "gophish-test"
	testOIDCClientSecret = "gophish-test-secret"
	// testOIDCRedirectURL is registered with the fake identity provider. The
	// test client sends requests for it to the test admin server instead.
	testOIDCRedirectURL = "https://gophish.test/login/oidc/callback"
)

// fakeIdP is a minimal OpenID Connect identity provider. Its authorization
// endpoint signs in whoever is in claims without asking.
type fakeIdP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey

	mu      sync.Mutex
	subject string
	claims  map[string]interface{}
	// pending holds the nonce and PKCE challenge for each issued code
	pending map[string][2]string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("error generating key: %v", err)
	}
	idp := &fakeIdP{t: t, key: key, pending: map[string][2]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("/jwks", idp.jwks)
	mux.HandleFunc("/authorize", idp.authorize)
	mux.HandleFunc("/token", idp.token)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// signIn sets who the identity provider signs in next.
func (idp *fakeIdP) signIn(subject string, claims map[string]interface{}) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.subject = subject
	idp.claims = claims
}

func (idp *fakeIdP) discovery(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"issuer":                                idp.server.URL,
		"authorization_endpoint":                idp.server.URL + "/authorize",
		"token_endpoint":                        idp.server.URL + "/token",
		"jwks_uri":                              idp.server.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (idp *fakeIdP) jwks(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &idp.key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"},
	}})
}

func (idp *fakeIdP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != testOIDCClientID || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := auth.GenerateSecureKey(16)
	idp.mu.Lock()
	idp.pending[code] = [2]string{q.Get("nonce"), q.Get("code_challenge")}
	idp.mu.Unlock()
	redirect, _ := url.Parse(q.Get("redirect_uri"))
	redirect.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (idp *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.FormValue("client_id"), r.FormValue("client_secret")
	}
	if id != testOIDCClientID || secret != testOIDCClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	idp.mu.Lock()
	pending, ok := idp.pending[r.FormValue("code")]
	delete(idp.pending, r.FormValue("code"))
	subject, claims := idp.subject, idp.claims
	idp.mu.Unlock()
	sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != pending[1] {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	payload := map[string]interface{}{
		"iss":   idp.server.URL,
		"sub":   subject,
		"aud":   testOIDCClientID,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": pending[0],
	}
	for k, v := range claims {
		payload[k] = v
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		idp.t.Fatalf("error creating signer: %v", err)
	}
	body, _ := json.Marshal(payload)
	jws, err := signer.Sign(body)
	if err != nil {
		idp.t.Fatalf("error signing id_token: %v", err)
	}
	idToken, _ := jws.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": "access",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

// setupOIDCTest sets up a test whose admin server signs users in through a
// fake identity provider. The configuration can be adjusted with configure.
func setupOIDCTest(t *testing.T, configure func(*config.OIDC)) (*testContext, *fakeIdP) {
	ctx := setupTwoFactorTest(t)
	idp := newFakeIdP(t)
	conf := &config.OIDC{
		Issuer:       idp.server.URL,
		ClientID:     testOIDCClientID,
		ClientSecret: testOIDCClientSecret,
		RedirectURL:  testOIDCRedirectURL,
	}
	if configure != nil {
		configure(conf)
	}
	ctx.config.AdminConf.OIDC = conf
	ctx.adminServer.Close()
	server := httptest.NewUnstartedServer(NewAdminServer(ctx.config.AdminConf,
		WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100)))).server.Handler)
	server.StartTLS()
	ctx.adminServer = server
	return ctx, idp
}

// ssoLogin signs in through the identity provider and returns the admin
// server's final response.
func ssoLogin(t *testing.T, ctx *testContext, client *http.Client, next string) *http.Response {
	start := ctx.adminServer.URL + "/login/oidc"
	if next != "" {
		start += "?" + url.Values{"next": {next}}.Encode()
	}
	resp, err := client.Get(start)
	if err != nil {
		t.Fatalf("error starting single sign-on: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect to the identity provider, got %d", resp.StatusCode)
	}
	resp, err = client.Get(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("error requesting the identity provider: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect back from the identity provider, got %d", resp.StatusCode)
	}
	return ssoCallback(t, ctx, client, resp.Header.Get("Location"))
}

// ssoCallback requests the admin server's callback with the query from
// location, which points at the registered redirect URL.
func ssoCallback(t *testing.T, ctx *testContext, client *http.Client, location string) *http.Response {
	loc, err := url.Parse(location)
	if err != nil {
		t.Fatalf("error parsing callback URL: %v", err)
	}
	resp, err := client.Get(ctx.adminServer.URL + loc.Path + "?" + loc.RawQuery)
	if err != nil {
		t.Fatalf("error requesting the callback: %v", err)
	}
	return resp
}

// expectSSORedirect checks that resp redirects to path.
func expectSSORedirect(t *testing.T, resp *http.Response, path string) {
	t.Helper()
	loc, _ := resp.Location()
	if resp.StatusCode != http.StatusFound || loc == nil || loc.Path != path {
		t.Fatalf("expected redirect to %s, got %d %v", path, resp.StatusCode, loc)
	}
}

// expectLoggedIn checks whether the client can reach a page that requires a
// login.
func expectLoggedIn(t *testing.T, ctx *testContext, client *http.Client, loggedIn bool) {
	t.Helper()
	resp, err := client.Get(ctx.adminServer.URL + "/campaigns")
	if err != nil {
		t.Fatalf("error requesting /campaigns: %v", err)
	}
	if got := resp.StatusCode == http.StatusOK; got != loggedIn {
		t.Fatalf("expected logged in to be %v, got status %d", loggedIn, resp.StatusCode)
	}
}

// createSSOUser creates a standard user with the given username.
func createSSOUser(t *testing.T, username string) models.User {
	role, err := models.GetRoleBySlug(models.RoleUser)
	if err != nil {
		t.Fatalf("error getting role: %v", err)
	}
	u := models.User{Username: username, Hash: "x", ApiKey: auth.GenerateSecureKey(auth.APIKeyLength), Role: role, RoleID: role.ID}
	if err := models.PutUser(&u); err != nil {
		t.Fatalf("error creating user: %v", err)
	}
	return u
}

func TestSSOButtonShown(t *testing.T) {
	ctx, _ := setupOIDCTest(t, func(c *config.OIDC) { c.ButtonLabel = "Sign in with Example" })
	defer tearDown(t, ctx)
	resp, err := ctx.adminServer.Client().Get(ctx.adminServer.URL + "/login?next=/campaigns")
	if err != nil {
		t.Fatalf("error requesting /login: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("error parsing /login: %v", err)
	}
	link := doc.Find("a[href^='/login/oidc']")
	if strings.TrimSpace(link.Text()) != "Sign in with Example" {
		t.Fatalf("expected the single sign-on button, got %q", link.Text())
	}
	if href, _ := link.Attr("href"); href != "/login/oidc?next=%2fcampaigns" && href != "/login/oidc?next=/campaigns" {
		t.Fatalf("unexpected single sign-on link %q", href)
	}
}

func TestSSOButtonHiddenWithoutConfig(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	resp, err := ctx.adminServer.Client().Get(ctx.adminServer.URL + "/login")
	if err != nil {
		t.Fatalf("error requesting /login: %v", err)
	}
	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	if doc.Find("a[href^='/login/oidc']").Length() != 0 {
		t.Fatal("expected no single sign-on button")
	}
	resp, err = ctx.adminServer.Client().Get(ctx.adminServer.URL + "/login/oidc")
	if err != nil {
		t.Fatalf("error requesting /login/oidc: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected /login/oidc to be missing, got %d", resp.StatusCode)
	}
}

func TestSSOLinksExistingUser(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	u := createSSOUser(t, "alice@example.com")
	idp.signIn("alice-id", map[string]interface{}{"email": "alice@example.com", "email_verified": true})

	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, "/campaigns"), "/campaigns")
	expectLoggedIn(t, ctx, client, true)
	got, _ := models.GetUser(u.Id)
	if got.OIDCIdentity != idp.server.URL+" alice-id" {
		t.Fatalf("expected the account to be linked, got %q", got.OIDCIdentity)
	}
	if got.LastLogin.IsZero() {
		t.Fatal("expected the last login time to be recorded")
	}

	// Once linked, the account follows the identity, not the email address
	idp.signIn("alice-id", map[string]interface{}{"email": "alice@new.example.com", "email_verified": true})
	client = newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/")
	expectLoggedIn(t, ctx, client, true)
}

func TestSSORejectsUnknownUser(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	idp.signIn("mallory-id", map[string]interface{}{"email": "mallory@example.com"})
	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/login")
	expectLoggedIn(t, ctx, client, false)
	if _, err := models.GetUserByUsername("mallory@example.com"); err == nil {
		t.Fatal("expected no account to be created")
	}
}

func TestSSOAutoCreatesUser(t *testing.T) {
	ctx, idp := setupOIDCTest(t, func(c *config.OIDC) {
		c.AutoCreateUsers = true
		c.AllowedDomains = []string{"example.com"}
	})
	defer tearDown(t, ctx)
	idp.signIn("bob-id", map[string]interface{}{"email": "bob@Example.com", "email_verified": true})
	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/")
	expectLoggedIn(t, ctx, client, true)
	u, err := models.GetUserByUsername("bob@Example.com")
	if err != nil {
		t.Fatalf("expected an account to be created: %v", err)
	}
	if u.Role.Slug != models.RoleUser || u.OIDCIdentity != idp.server.URL+" bob-id" {
		t.Fatalf("unexpected account: role %q identity %q", u.Role.Slug, u.OIDCIdentity)
	}

	// People outside the allowed domains are turned away
	idp.signIn("eve-id", map[string]interface{}{"email": "eve@elsewhere.com", "email_verified": true})
	client = newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSSORejectsUnverifiedEmail(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice@example.com")
	idp.signIn("attacker-id", map[string]interface{}{"email": "alice@example.com", "email_verified": false})
	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSSORejectsAccountLinkedElsewhere(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	u := createSSOUser(t, "alice@example.com")
	u.OIDCIdentity = idp.server.URL + " alice-id"
	models.PutUser(&u)
	idp.signIn("other-id", map[string]interface{}{"email": "alice@example.com", "email_verified": true})
	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSSORejectsLockedAccount(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	u := createSSOUser(t, "alice@example.com")
	u.AccountLocked = true
	models.PutUser(&u)
	idp.signIn("alice-id", map[string]interface{}{"email": "alice@example.com"})
	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoLogin(t, ctx, client, ""), "/login")
	expectLoggedIn(t, ctx, client, false)
	if got, _ := models.GetUser(u.Id); got.OIDCIdentity != "" {
		t.Fatalf("expected the locked account not to be linked, got %q", got.OIDCIdentity)
	}
}

func TestSSORequiresTOTP(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	u := createSSOUser(t, "alice@example.com")
	secret := auth.GenerateTOTPSecret()
	if _, err := models.EnableTOTP(&u, secret, 0); err != nil {
		t.Fatalf("error enabling two-factor authentication: %v", err)
	}
	idp.signIn("alice-id", map[string]interface{}{"email": "alice@example.com"})
	client := newJarClient(t, ctx)
	resp := ssoLogin(t, ctx, client, "/campaigns")
	expectSSORedirect(t, resp, "/login/2fa")
	expectLoggedIn(t, ctx, client, false)

	loc, _ := resp.Location()
	code, _ := auth.TOTPCode(secret, time.Now())
	resp = postForm(t, ctx, client, loc.RequestURI(), url.Values{"code": {code}})
	expectSSORedirect(t, resp, "/campaigns")
	expectLoggedIn(t, ctx, client, true)
}

func TestSSORejectsWrongState(t *testing.T) {
	ctx, idp := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice@example.com")
	idp.signIn("alice-id", map[string]interface{}{"email": "alice@example.com"})
	client := newJarClient(t, ctx)
	resp, err := client.Get(ctx.adminServer.URL + "/login/oidc")
	if err != nil {
		t.Fatalf("error starting single sign-on: %v", err)
	}
	resp, err = client.Get(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("error requesting the identity provider: %v", err)
	}
	// A callback carrying someone else's state is refused...
	callback, _ := url.Parse(resp.Header.Get("Location"))
	q := callback.Query()
	q.Set("state", "forged")
	callback.RawQuery = q.Encode()
	expectSSORedirect(t, ssoCallback(t, ctx, client, callback.String()), "/login")
	expectLoggedIn(t, ctx, client, false)
	// ...and uses up the sign in attempt, so the real callback fails too
	expectSSORedirect(t, ssoCallback(t, ctx, client, resp.Header.Get("Location")), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestSSOCallbackWithoutLoginStarted(t *testing.T) {
	ctx, _ := setupOIDCTest(t, nil)
	defer tearDown(t, ctx)
	client := newJarClient(t, ctx)
	expectSSORedirect(t, ssoCallback(t, ctx, client, testOIDCRedirectURL+"?code=x&state="), "/login")
	expectLoggedIn(t, ctx, client, false)
}

func TestInAllowedDomain(t *testing.T) {
	domains := []string{"example.com", "@corp.example.org"}
	cases := map[string]bool{
		"a@example.com":         true,
		"a@EXAMPLE.com":         true,
		"a@corp.example.org":    true,
		"a@evil.com":            false,
		"a@example.com.evil.io": false,
		"a@sub.example.com":     false,
		"example.com":           false,
	}
	for email, expected := range cases {
		if got := inAllowedDomain(email, domains); got != expected {
			t.Fatalf("inAllowedDomain(%q) = %v, expected %v", email, got, expected)
		}
	}
}

func TestSSODisablePasswordLogin(t *testing.T) {
	ctx, idp := setupOIDCTest(t, func(c *config.OIDC) { c.DisablePasswordLogin = true })
	defer tearDown(t, ctx)
	client := newJarClient(t, ctx)
	resp, err := client.Get(ctx.adminServer.URL + "/login")
	if err != nil {
		t.Fatalf("error requesting /login: %v", err)
	}
	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	if doc.Find("input[name='password']").Length() != 0 {
		t.Fatal("expected the password form to be hidden")
	}
	if doc.Find("a[href^='/login/oidc']").Length() != 1 {
		t.Fatal("expected the single sign-on button")
	}
	// A password login is refused even with the right password
	resp = postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	if resp.StatusCode == http.StatusFound {
		t.Fatalf("expected the password login to be refused, got %d", resp.StatusCode)
	}
	expectLoggedIn(t, ctx, client, false)

	// Signing in through the identity provider skips a pending password
	// change, since the password can't be used
	u := createSSOUser(t, "alice@example.com")
	u.PasswordChangeRequired = true
	models.PutUser(&u)
	idp.signIn("alice-id", map[string]interface{}{"email": "alice@example.com"})
	expectSSORedirect(t, ssoLogin(t, ctx, client, "/campaigns"), "/campaigns")
	expectLoggedIn(t, ctx, client, true)
}
