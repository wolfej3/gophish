package controllers

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// bodyOf reads and returns a response body.
func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(b)
}

// TestLoginNoUserMatchesWrongPassword ensures an unknown username and a wrong
// password for an existing user give the same status and message, so the
// response doesn't reveal which usernames exist (CVE-2026-107269).
func TestLoginNoUserMatchesWrongPassword(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)

	unknown := attemptLogin(t, ctx, nil, "does-not-exist", "whatever", "")
	wrong := attemptLogin(t, ctx, nil, "admin", "definitely-wrong", "")

	if unknown.StatusCode != wrong.StatusCode {
		t.Fatalf("status differs: unknown %d vs wrong %d", unknown.StatusCode, wrong.StatusCode)
	}
	// Bodies differ only by the per-request CSRF token; both must carry the
	// same generic message and neither should reveal which case it was.
	ub, wb := bodyOf(t, unknown), bodyOf(t, wrong)
	for _, b := range []string{ub, wb} {
		if !strings.Contains(b, "Invalid Username/Password") {
			t.Fatal("expected the generic invalid-credentials message")
		}
	}
}
