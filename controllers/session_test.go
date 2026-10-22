package controllers

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/models"
)

// browser is an HTTP client with its own cookie jar, standing in for a
// separate browser logged in to the admin server.
type browser struct {
	t      *testing.T
	ctx    *testContext
	client *http.Client
}

// setupSessionTest sets up the test servers with the admin's initial password
// change already done, so that every page is reachable after logging in.
func setupSessionTest(t *testing.T) *testContext {
	ctx := setupTest(t)
	admin, err := models.GetUser(1)
	if err != nil {
		t.Fatalf("error getting admin user: %v", err)
	}
	admin.PasswordChangeRequired = false
	if err = models.PutUser(&admin); err != nil {
		t.Fatalf("error updating admin user: %v", err)
	}
	return ctx
}

func newBrowser(t *testing.T, ctx *testContext) *browser {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("error creating cookie jar: %v", err)
	}
	return &browser{
		t:   t,
		ctx: ctx,
		client: &http.Client{
			Transport: ctx.adminServer.Client().Transport,
			Jar:       jar,
		},
	}
}

// csrfToken loads the page at path and returns the CSRF token in its form.
func (b *browser) csrfToken(path string) string {
	resp, err := b.client.Get(b.ctx.adminServer.URL + path)
	if err != nil {
		b.t.Fatalf("error requesting %s: %v", path, err)
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		b.t.Fatalf("error parsing %s: %v", path, err)
	}
	token, ok := doc.Find("input[name='csrf_token']").First().Attr("value")
	if !ok {
		b.t.Fatalf("no csrf_token found on %s", path)
	}
	return token
}

// postForm submits a form to path with the page's CSRF token and the
// Referer a browser would send.
func (b *browser) postForm(path string, values url.Values) *http.Response {
	values.Set("csrf_token", b.csrfToken(path))
	req, err := http.NewRequest(http.MethodPost, b.ctx.adminServer.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		b.t.Fatalf("error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", b.ctx.adminServer.URL+path)
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatalf("error posting to %s: %v", path, err)
	}
	resp.Body.Close()
	return resp
}

func (b *browser) login(username, password string) {
	b.postForm("/login", url.Values{"username": {username}, "password": {password}})
	if !b.loggedIn() {
		b.t.Fatalf("expected to be logged in as %s", username)
	}
}

// loggedIn reports whether the browser can load an authenticated page
// rather than being redirected to the login page.
func (b *browser) loggedIn() bool {
	resp, err := b.client.Get(b.ctx.adminServer.URL + "/")
	if err != nil {
		b.t.Fatalf("error requesting dashboard: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK && resp.Request.URL.Path != "/login"
}

// sessionCookie returns the browser's current session token.
func (b *browser) sessionCookie() string {
	u, _ := url.Parse(b.ctx.adminServer.URL)
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == "gophish" {
			return c.Value
		}
	}
	return ""
}

// withSessionCookie returns a new browser holding only the given session
// token, as if it had been copied from another browser.
func (b *browser) withSessionCookie(token string) *browser {
	other := newBrowser(b.t, b.ctx)
	u, _ := url.Parse(b.ctx.adminServer.URL)
	other.client.Jar.SetCookies(u, []*http.Cookie{{Name: "gophish", Value: token, Path: "/"}})
	return other
}

func TestLogoutRevokesSession(t *testing.T) {
	ctx := setupSessionTest(t)
	defer tearDown(t, ctx)
	b := newBrowser(t, ctx)
	b.login("admin", "gophish")
	token := b.sessionCookie()

	resp, err := b.client.Get(ctx.adminServer.URL + "/logout")
	if err != nil {
		t.Fatalf("error logging out: %v", err)
	}
	resp.Body.Close()
	if b.loggedIn() {
		t.Fatal("expected to be logged out")
	}
	// A copy of the cookie from before logging out must no longer work
	if b.withSessionCookie(token).loggedIn() {
		t.Fatal("session token still valid after logout")
	}
}

func TestLoginIssuesNewSessionToken(t *testing.T) {
	ctx := setupSessionTest(t)
	defer tearDown(t, ctx)
	// Plant a valid, unauthenticated session in the browser, as an attacker
	// attempting session fixation would. Logging in must replace its token so
	// the planted token never becomes authenticated.
	planted := "planted-session-token"
	var data bytes.Buffer
	values := map[interface{}]interface{}{"planted": true}
	if err := gob.NewEncoder(&data).Encode(values); err != nil {
		t.Fatalf("error encoding session: %v", err)
	}
	err := models.PutSession(planted, nil, data.Bytes(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("error planting session: %v", err)
	}
	b := newBrowser(t, ctx).withSessionCookie(planted)
	b.login("admin", "gophish")
	if b.sessionCookie() == planted {
		t.Fatal("session token wasn't replaced on login")
	}
	if b.withSessionCookie(planted).loggedIn() {
		t.Fatal("planted session token is logged in")
	}
}

func TestUnknownSessionTokenIgnored(t *testing.T) {
	ctx := setupSessionTest(t)
	defer tearDown(t, ctx)
	b := newBrowser(t, ctx).withSessionCookie("not-a-real-session")
	if b.loggedIn() {
		t.Fatal("unknown session token was accepted")
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	ctx := setupSessionTest(t)
	defer tearDown(t, ctx)
	current := newBrowser(t, ctx)
	current.login("admin", "gophish")
	other := newBrowser(t, ctx)
	other.login("admin", "gophish")

	newPassword := "a-new-Passw0rd!"
	resp := current.postForm("/settings", url.Values{
		"current_password":     {"gophish"},
		"new_password":         {newPassword},
		"confirm_new_password": {newPassword},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status changing password: %d", resp.StatusCode)
	}
	if !current.loggedIn() {
		t.Fatal("the session that changed the password was signed out")
	}
	if other.loggedIn() {
		t.Fatal("other session still valid after password change")
	}
}

func TestSessionSurvivesRestart(t *testing.T) {
	ctx := setupSessionTest(t)
	defer tearDown(t, ctx)
	b := newBrowser(t, ctx)
	b.login("admin", "gophish")
	token := b.sessionCookie()

	// Start a new session store and admin server against the same database,
	// as if Gophish had restarted.
	ctx.adminServer.Close()
	mid.Store = mid.NewDBStore()
	ctx.adminServer = newTestAdminServer(ctx)
	b = newBrowser(t, ctx).withSessionCookie(token)
	if !b.loggedIn() {
		t.Fatal("session token not valid after restart")
	}
}

func TestLogoutWithPasswordChangeRequired(t *testing.T) {
	// setupTest leaves the admin's initial password change pending, which
	// redirects every other page to the password reset page.
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	b := newBrowser(t, ctx)
	b.login("admin", "gophish")
	resp, err := b.client.Get(ctx.adminServer.URL + "/logout")
	if err != nil {
		t.Fatalf("error logging out: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("expected to land on the login page, got %s", resp.Request.URL.Path)
	}
	if b.loggedIn() {
		t.Fatal("expected to be logged out")
	}
}

func TestAnonymousVisitorsGetNoSession(t *testing.T) {
	ctx := setupSessionTest(t)
	defer tearDown(t, ctx)
	b := newBrowser(t, ctx)
	// Neither viewing the login page nor failing to log in should store a
	// session for someone who isn't logged in.
	b.postForm("/login", url.Values{"username": {"admin"}, "password": {"wrong"}})
	if token := b.sessionCookie(); token != "" {
		t.Fatalf("unexpected session cookie for anonymous visitor: %q", token)
	}
}
