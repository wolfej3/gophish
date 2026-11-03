package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// getLoginPage fetches the login page and returns the response, whose
// Set-Cookie carries the session the login POST will use.
func getLoginPage(t *testing.T, ctx *testContext) *http.Response {
	resp, err := ctx.adminServer.Client().Get(fmt.Sprintf("%s/login", ctx.adminServer.URL))
	if err != nil {
		t.Fatalf("error requesting the /login endpoint: %v", err)
	}
	got := resp.StatusCode
	expected := http.StatusOK
	if got != expected {
		t.Fatalf("invalid status code received. expected %d got %d", expected, got)
	}
	return resp
}

// newLoginRequest builds a login form submission, mirroring what a browser
// sends when submitting the login page. Callers add Origin/Sec-Fetch-Site
// headers to simulate a cross-origin submission.
func newLoginRequest(t *testing.T, ctx *testContext, resp *http.Response, username, password, optionalPath string) *http.Request {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/login%s", ctx.adminServer.URL, optionalPath), strings.NewReader(url.Values{
		"username": {username},
		"password": {password},
	}.Encode()))
	if err != nil {
		t.Fatalf("error creating new /login request: %v", err)
	}

	req.Header.Set("Cookie", resp.Header.Get("Set-Cookie"))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func attemptLogin(t *testing.T, ctx *testContext, client *http.Client, username, password, optionalPath string) *http.Response {
	resp := getLoginPage(t, ctx)
	if client == nil {
		client = ctx.adminServer.Client()
	}
	req := newLoginRequest(t, ctx, resp, username, password, optionalPath)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error requesting the /login endpoint: %v", err)
	}
	return resp
}

// TestLoginCrossOriginRejected ensures a cross-origin login POST — signalled
// either by Sec-Fetch-Site or by a foreign Origin header — is rejected by the
// standard library's cross-origin protection.
func TestLoginCrossOriginRejected(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	cases := map[string]map[string]string{
		"Sec-Fetch-Site cross-site": {"Sec-Fetch-Site": "cross-site"},
		"foreign Origin":            {"Origin": "https://evil.example.com"},
	}
	for name, headers := range cases {
		resp := getLoginPage(t, ctx)
		req := newLoginRequest(t, ctx, resp, "admin", "gophish", "")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := ctx.adminServer.Client().Do(req)
		if err != nil {
			t.Fatalf("error requesting the /login endpoint: %v", err)
		}
		got := resp.StatusCode
		expected := http.StatusForbidden
		if got != expected {
			t.Fatalf("%s: invalid status code received. expected %d got %d", name, expected, got)
		}
	}
}

// TestLoginSameOriginAllowed ensures a same-origin login POST passes the
// cross-origin check and reaches authentication (a 403 would mean the check
// wrongly rejected it).
func TestLoginSameOriginAllowed(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp := getLoginPage(t, ctx)
	req := newLoginRequest(t, ctx, resp, "admin", "gophish", "")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := ctx.adminServer.Client().Do(req)
	if err != nil {
		t.Fatalf("error requesting the /login endpoint: %v", err)
	}
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("same-origin login unexpectedly rejected with %d", http.StatusForbidden)
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
