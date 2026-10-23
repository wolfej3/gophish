package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/middleware/ratelimit"
	"github.com/gophish/gophish/models"
)

// newJarClient returns a client for the admin server that keeps cookies and
// doesn't follow redirects.
func newJarClient(t *testing.T, ctx *testContext) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("error creating cookie jar: %v", err)
	}
	return &http.Client{
		Transport: ctx.adminServer.Client().Transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// postForm fetches the page at path for its CSRF token, then submits the
// provided values to it.
func postForm(t *testing.T, ctx *testContext, client *http.Client, path string, values url.Values) *http.Response {
	return postFormTo(t, ctx, client, path, path, values)
}

// decodeJSON decodes the JSON body of resp into v.
func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("error decoding response: %v", err)
	}
}

// setupTwoFactorTest sets up a test whose admin server allows enough requests
// per minute to walk through two-factor logins, and whose admin user doesn't
// need to change their password first.
func setupTwoFactorTest(t *testing.T) *testContext {
	ctx := setupTest(t)
	ctx.adminServer.Close()
	server := httptest.NewUnstartedServer(NewAdminServer(ctx.config.AdminConf,
		WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100)))).server.Handler)
	server.Config.Addr = ctx.config.AdminConf.ListenURL
	server.StartTLS()
	ctx.adminServer = server
	u, err := models.GetUser(1)
	if err != nil {
		t.Fatalf("error getting admin user: %v", err)
	}
	u.PasswordChangeRequired = false
	models.PutUser(&u)
	return ctx
}

// enableAdminTOTP turns on two-factor authentication for the admin user,
// returning the secret and recovery codes.
func enableAdminTOTP(t *testing.T) (string, []string) {
	u, err := models.GetUser(1)
	if err != nil {
		t.Fatalf("error getting admin user: %v", err)
	}
	secret := auth.GenerateTOTPSecret()
	codes, err := models.EnableTOTP(&u, secret, 0)
	if err != nil {
		t.Fatalf("error enabling two-factor authentication: %v", err)
	}
	return secret, codes
}

// loginWithPassword submits the admin's password and checks that they're
// sent to enter a two-factor code.
func loginWithPassword(t *testing.T, ctx *testContext, client *http.Client, query string) {
	resp := postForm(t, ctx, client, "/login"+query, url.Values{"username": {"admin"}, "password": {"gophish"}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unexpected status code. expected %d got %d", http.StatusFound, resp.StatusCode)
	}
	loc, _ := resp.Location()
	if loc.Path != "/login/2fa" {
		t.Fatalf("expected redirect to /login/2fa, got %s", loc)
	}
	// The password alone doesn't log the user in
	resp, err := client.Get(ctx.adminServer.URL + "/campaigns")
	if err != nil {
		t.Fatalf("error requesting /campaigns: %v", err)
	}
	loc, _ = resp.Location()
	if resp.StatusCode != http.StatusTemporaryRedirect || loc.Path != "/login" {
		t.Fatalf("expected to be redirected to login before entering a code, got %d %s", resp.StatusCode, loc)
	}
}

func TestLoginWithoutTOTPStillWorks(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := newJarClient(t, ctx)
	resp := postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	loc, _ := resp.Location()
	if resp.StatusCode != http.StatusFound || loc.Path != "/" {
		t.Fatalf("expected redirect to /, got %d %s", resp.StatusCode, loc)
	}
}

func TestLoginWithTOTP(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	secret, _ := enableAdminTOTP(t)
	client := newJarClient(t, ctx)
	loginWithPassword(t, ctx, client, "?next=/campaigns")

	code, _ := auth.TOTPCode(secret, time.Now())
	resp := postForm(t, ctx, client, "/login/2fa?next=/campaigns", url.Values{"code": {code}})
	loc, _ := resp.Location()
	if resp.StatusCode != http.StatusFound || loc.Path != "/campaigns" {
		t.Fatalf("expected redirect to /campaigns, got %d %s", resp.StatusCode, loc)
	}
	resp, err := client.Get(ctx.adminServer.URL + "/campaigns")
	if err != nil {
		t.Fatalf("error requesting /campaigns: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected to be logged in, got %d", resp.StatusCode)
	}

	// The same code can't be used to log in again
	other := newJarClient(t, ctx)
	loginWithPassword(t, ctx, other, "")
	resp = postForm(t, ctx, other, "/login/2fa", url.Values{"code": {code}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected replayed code to be rejected, got %d", resp.StatusCode)
	}
}

func TestLoginWithRecoveryCode(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	_, codes := enableAdminTOTP(t)
	client := newJarClient(t, ctx)
	loginWithPassword(t, ctx, client, "")
	resp := postForm(t, ctx, client, "/login/2fa", url.Values{"code": {strings.ToUpper(codes[0])}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected recovery code to be accepted, got %d", resp.StatusCode)
	}
	remaining, err := models.CountRecoveryCodes(1)
	if err != nil {
		t.Fatalf("error counting recovery codes: %v", err)
	}
	if remaining != auth.RecoveryCodeCount-1 {
		t.Fatalf("unexpected recovery codes remaining. expected %d got %d", auth.RecoveryCodeCount-1, remaining)
	}
	// Each recovery code works only once
	other := newJarClient(t, ctx)
	loginWithPassword(t, ctx, other, "")
	resp = postForm(t, ctx, other, "/login/2fa", url.Values{"code": {codes[0]}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected used recovery code to be rejected, got %d", resp.StatusCode)
	}
}

func TestLoginTOTPTooManyAttempts(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	enableAdminTOTP(t)
	client := newJarClient(t, ctx)
	loginWithPassword(t, ctx, client, "")
	for i := 1; i < maxSecondFactorAttempts; i++ {
		resp := postForm(t, ctx, client, "/login/2fa", url.Values{"code": {"000000"}})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected %d got %d", i, http.StatusUnauthorized, resp.StatusCode)
		}
	}
	resp := postForm(t, ctx, client, "/login/2fa", url.Values{"code": {"000000"}})
	loc, _ := resp.Location()
	if resp.StatusCode != http.StatusFound || loc.Path != "/login" {
		t.Fatalf("expected to be sent back to /login, got %d %s", resp.StatusCode, loc)
	}
	// Even a correct code is no longer accepted without the password
	resp, err := client.Get(ctx.adminServer.URL + "/login/2fa")
	if err != nil {
		t.Fatalf("error requesting /login/2fa: %v", err)
	}
	loc, _ = resp.Location()
	if resp.StatusCode != http.StatusFound || loc.Path != "/login" {
		t.Fatalf("expected the pending login to be cleared, got %d %s", resp.StatusCode, loc)
	}
}

func TestTwoFactorSettings(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := newJarClient(t, ctx)
	resp := postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("error logging in: %d", resp.StatusCode)
	}
	post := func(path string, values url.Values) *http.Response {
		// The settings page carries the CSRF token for these requests
		return postFormTo(t, ctx, client, "/settings", path, values)
	}

	if resp := post("/settings/2fa/setup", url.Values{"current_password": {"wrong"}}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected setup with the wrong password to fail, got %d", resp.StatusCode)
	}
	resp = post("/settings/2fa/setup", url.Values{"current_password": {"gophish"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code for setup: %d", resp.StatusCode)
	}
	var setup twoFactorResponse
	decodeJSON(t, resp, &setup)
	if setup.Secret == "" || !strings.HasPrefix(setup.QRCode, "data:image/png;base64,") {
		t.Fatalf("unexpected setup response: %+v", setup)
	}
	if resp := post("/settings/2fa/enable", url.Values{"code": {"000000"}}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected enabling with a wrong code to fail, got %d", resp.StatusCode)
	}
	code, _ := auth.TOTPCode(setup.Secret, time.Now())
	resp = post("/settings/2fa/enable", url.Values{"code": {code}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code for enable: %d", resp.StatusCode)
	}
	var enabled twoFactorResponse
	decodeJSON(t, resp, &enabled)
	if len(enabled.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Fatalf("expected %d recovery codes, got %d", auth.RecoveryCodeCount, len(enabled.RecoveryCodes))
	}
	u, _ := models.GetUser(1)
	if !u.TOTPEnabled || u.TOTPSecret != setup.Secret {
		t.Fatal("expected two-factor authentication to be enabled")
	}

	// Turning it off needs a current code, and the enable code was used
	if resp := post("/settings/2fa/disable", url.Values{"current_password": {"gophish"}, "code": {code}}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected reused code to be rejected, got %d", resp.StatusCode)
	}
	resp = post("/settings/2fa/disable", url.Values{"current_password": {"gophish"}, "code": {enabled.RecoveryCodes[0]}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code for disable: %d", resp.StatusCode)
	}
	u, _ = models.GetUser(1)
	if u.TOTPEnabled || u.TOTPSecret != "" {
		t.Fatal("expected two-factor authentication to be disabled")
	}
	if remaining, _ := models.CountRecoveryCodes(1); remaining != 0 {
		t.Fatalf("expected recovery codes to be deleted, got %d", remaining)
	}
}

// postFormTo fetches the page at pagePath for its CSRF token, then submits
// the provided values to path.
func postFormTo(t *testing.T, ctx *testContext, client *http.Client, pagePath, path string, values url.Values) *http.Response {
	resp, err := client.Get(ctx.adminServer.URL + pagePath)
	if err != nil {
		t.Fatalf("error requesting %s: %v", pagePath, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code requesting %s. expected %d got %d", pagePath, http.StatusOK, resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("error parsing %s: %v", pagePath, err)
	}
	token, ok := doc.Find("input[name='csrf_token']").First().Attr("value")
	if !ok {
		t.Fatalf("unable to find csrf_token on %s", pagePath)
	}
	values.Set("csrf_token", token)
	req, err := http.NewRequest("POST", ctx.adminServer.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", fmt.Sprintf("%s%s", ctx.adminServer.URL, pagePath))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("error posting to %s: %v", path, err)
	}
	return resp
}
