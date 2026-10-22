package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// getLoginToken fetches the login page, returning the response (which carries
// the CSRF cookie) and the CSRF token embedded in the form.
func getLoginToken(t *testing.T, ctx *testContext) (*http.Response, string) {
	resp, err := ctx.adminServer.Client().Get(fmt.Sprintf("%s/login", ctx.adminServer.URL))
	if err != nil {
		t.Fatalf("error requesting the /login endpoint: %v", err)
	}
	got := resp.StatusCode
	expected := http.StatusOK
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		t.Fatalf("error parsing /login response body")
	}
	elem := doc.Find("input[name='csrf_token']").First()
	token, ok := elem.Attr("value")
	if !ok {
		t.Fatal("unable to find csrf_token value in login response")
	}
	return resp, token
}

// newLoginRequest builds a login form submission with the provided Referer,
// mirroring what a browser sends when submitting the login page.
func newLoginRequest(t *testing.T, ctx *testContext, resp *http.Response, token, username, password, optionalPath, referer string) *http.Request {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/login%s", ctx.adminServer.URL, optionalPath), strings.NewReader(url.Values{
		"username":   {username},
		"password":   {password},
		"csrf_token": {token},
	}.Encode()))
	if err != nil {
		t.Fatalf("error creating new /login request: %v", err)
	}

	req.Header.Set("Cookie", resp.Header.Get("Set-Cookie"))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return req
}

func attemptLogin(t *testing.T, ctx *testContext, client *http.Client, username, password, optionalPath string) *http.Response {
	resp, token := getLoginToken(t, ctx)
	if client == nil {
		client = ctx.adminServer.Client()
	}
	req := newLoginRequest(t, ctx, resp, token, username, password, optionalPath, fmt.Sprintf("%s/login", ctx.adminServer.URL))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error requesting the /login endpoint: %v", err)
	}
	return resp
}

func TestLoginRefererRequired(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	referers := map[string]string{
		"missing":      "",
		"cross-origin": "https://evil.example.com/login",
		"plain http":   strings.Replace(ctx.adminServer.URL, "https://", "http://", 1) + "/login",
	}
	for name, referer := range referers {
		resp, token := getLoginToken(t, ctx)
		req := newLoginRequest(t, ctx, resp, token, "admin", "gophish", "", referer)
		resp, err := ctx.adminServer.Client().Do(req)
		if err != nil {
			t.Fatalf("error requesting the /login endpoint: %v", err)
		}
		got := resp.StatusCode
		expected := http.StatusForbidden
		if got != expected {
			t.Fatalf("%s referer: invalid status code received. expected %d got %d", name, expected, got)
		}
	}
}

func TestLoginCSRF(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp, err := ctx.adminServer.Client().PostForm(fmt.Sprintf("%s/login", ctx.adminServer.URL),
		url.Values{
			"username": {"admin"},
			"password": {"gophish"},
		})

	if err != nil {
		t.Fatalf("error requesting the /login endpoint: %v", err)
	}

	got := resp.StatusCode
	expected := http.StatusForbidden
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}
}

func TestInvalidCredentials(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp := attemptLogin(t, ctx, nil, "admin", "bogus", "")
	got := resp.StatusCode
	expected := http.StatusUnauthorized
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}
}

func TestSuccessfulLogin(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp := attemptLogin(t, ctx, nil, "admin", "gophish", "")
	got := resp.StatusCode
	expected := http.StatusOK
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}
}

func TestSuccessfulRedirect(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	next := "/campaigns"
	client := &http.Client{
		Transport: ctx.adminServer.Client().Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	resp := attemptLogin(t, ctx, client, "admin", "gophish", fmt.Sprintf("?next=%s", next))
	got := resp.StatusCode
	expected := http.StatusFound
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}
	url, err := resp.Location()
	if err != nil {
		t.Fatalf("error parsing response Location header: %v", err)
	}
	if url.Path != next {
		t.Fatalf("unexpected Location header received. expected %s got %s", next, url.Path)
	}
}

func TestAccountLocked(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp := attemptLogin(t, ctx, nil, "houdini", "gophish", "")
	got := resp.StatusCode
	expected := http.StatusUnauthorized
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}
}
