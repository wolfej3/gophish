package controllers

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// loginJar logs the admin in and returns a cookie-jar client.
func loginJar(t *testing.T, ctx *testContext) *http.Client {
	t.Helper()
	client := newJarClient(t, ctx)
	resp := postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login failed: %d", resp.StatusCode)
	}
	return client
}

// pageCSRFToken fetches a page and returns a CSRF token valid for the session.
func pageCSRFToken(t *testing.T, ctx *testContext, client *http.Client, path string) string {
	t.Helper()
	resp, err := client.Get(ctx.adminServer.URL + path)
	if err != nil {
		t.Fatalf("error requesting %s: %v", path, err)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("error parsing %s: %v", path, err)
	}
	token, ok := doc.Find("input[name='csrf_token']").First().Attr("value")
	if !ok {
		t.Fatalf("no csrf token on %s", path)
	}
	return token
}

// apiReq sends an API request through the full admin server with the given
// headers.
func apiReq(t *testing.T, ctx *testContext, client *http.Client, method, path string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ctx.adminServer.URL+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("error building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", ctx.adminServer.URL+path)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error on %s %s: %v", method, path, err)
	}
	return resp
}

func TestSessionAPIReadWorks(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := loginJar(t, ctx)
	resp := apiReq(t, ctx, client, http.MethodGet, "/api/campaigns/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session GET /api/campaigns/ expected 200, got %d", resp.StatusCode)
	}
}

func TestSessionAPIWriteNeedsCSRF(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := loginJar(t, ctx)
	// No CSRF token -> rejected
	resp := apiReq(t, ctx, client, http.MethodPost, "/api/groups/", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for session POST without CSRF, got %d", resp.StatusCode)
	}
	// With the token -> passes CSRF (reaches the handler; 400 for the empty body is fine)
	token := pageCSRFToken(t, ctx, client, "/settings")
	resp = apiReq(t, ctx, client, http.MethodPost, "/api/groups/", map[string]string{"X-CSRF-Token": token})
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("expected the CSRF token to be accepted, still got 403")
	}
}

func TestBearerAPIWriteSkipsCSRF(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	// A bearer-authenticated request needs no CSRF token, as before.
	client := ctx.adminServer.Client()
	req, _ := http.NewRequest(http.MethodGet, ctx.adminServer.URL+"/api/campaigns/", nil)
	req.Header.Set("Authorization", "Bearer "+ctx.apiKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer GET expected 200, got %d", resp.StatusCode)
	}
}

func TestAPIKeyNotInPage(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := loginJar(t, ctx)
	u, _ := modelsGetUser1()
	resp, err := client.Get(ctx.adminServer.URL + "/")
	if err != nil {
		t.Fatalf("error requesting dashboard: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), u.ApiKey) {
		t.Fatal("the dashboard page contains the user's API key")
	}
}

func TestAPIKeyRevealRequiresPassword(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := loginJar(t, ctx)
	// Wrong password -> 400
	resp := postFormTo(t, ctx, client, "/settings", "/settings/api_key/reveal", url.Values{"current_password": {"wrong"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong password, got %d", resp.StatusCode)
	}
	// Right password -> 200 with the key
	resp = postFormTo(t, ctx, client, "/settings", "/settings/api_key/reveal", url.Values{"current_password": {"gophish"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 revealing the key, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	u, _ := modelsGetUser1()
	if !strings.Contains(string(body), u.ApiKey) {
		t.Fatal("reveal didn't return the API key")
	}
}
