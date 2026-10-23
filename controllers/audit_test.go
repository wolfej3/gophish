package controllers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/models"
)

// auditEntries returns the audit log entries with the given action, newest
// first.
func auditEntries(t *testing.T, action string) []models.AuditLog {
	t.Helper()
	logs, err := models.GetAuditLogs(models.AuditLogQuery{Action: action})
	if err != nil {
		t.Fatalf("error getting audit log: %v", err)
	}
	return logs
}

func TestAuditLogins(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)

	postForm(t, ctx, newJarClient(t, ctx), "/login", url.Values{"username": {"nobody"}, "password": {"x"}})
	postForm(t, ctx, newJarClient(t, ctx), "/login", url.Values{"username": {"admin"}, "password": {"wrong"}})
	postForm(t, ctx, newJarClient(t, ctx), "/login", url.Values{"username": {"houdini"}, "password": {"gophish"}})
	failed := auditEntries(t, models.AuditLoginFailed)
	if len(failed) != 3 {
		t.Fatalf("expected 3 failed logins, got %d", len(failed))
	}
	expected := []struct{ username, details string }{
		{"houdini", "account locked"},
		{"admin", "invalid password"},
		{"nobody", "unknown username"},
	}
	for i, e := range expected {
		if failed[i].Username != e.username || failed[i].Details != e.details {
			t.Fatalf("unexpected failed login %d: %+v", i, failed[i])
		}
		if failed[i].IPAddress != "127.0.0.1" {
			t.Fatalf("expected the IP address to be recorded, got %q", failed[i].IPAddress)
		}
	}
	if failed[2].UserId != nil || failed[1].UserId == nil || *failed[1].UserId != 1 {
		t.Fatal("expected failed logins to record the account only when it exists")
	}

	client := newJarClient(t, ctx)
	postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	logins := auditEntries(t, models.AuditLogin)
	if len(logins) != 1 || logins[0].Username != "admin" || logins[0].Details != "password" {
		t.Fatalf("unexpected logins: %+v", logins)
	}
	client.Get(ctx.adminServer.URL + "/logout")
	if logouts := auditEntries(t, models.AuditLogout); len(logouts) != 1 || logouts[0].Username != "admin" {
		t.Fatalf("unexpected logouts: %+v", logouts)
	}
}

func TestAuditTwoFactorLogin(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	secret, _ := enableAdminTOTP(t)
	client := newJarClient(t, ctx)
	loginWithPassword(t, ctx, client, "")
	// The password alone isn't a login
	if logins := auditEntries(t, models.AuditLogin); len(logins) != 0 {
		t.Fatalf("expected no login yet, got %+v", logins)
	}
	postForm(t, ctx, client, "/login/2fa", url.Values{"code": {"000000"}})
	if failed := auditEntries(t, models.AuditLoginFailed); len(failed) != 1 || failed[0].Details != "invalid two-factor code" {
		t.Fatalf("unexpected failed logins: %+v", failed)
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	postForm(t, ctx, client, "/login/2fa", url.Values{"code": {code}})
	logins := auditEntries(t, models.AuditLogin)
	if len(logins) != 1 || logins[0].Details != "password and two-factor code" {
		t.Fatalf("unexpected logins: %+v", logins)
	}
}

func TestAuditImpersonation(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	createSSOUser(t, "alice")
	client := newJarClient(t, ctx)
	postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	resp := postFormTo(t, ctx, client, "/settings", "/impersonate", url.Values{"username": {"alice"}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unexpected status impersonating: %d", resp.StatusCode)
	}
	entries := auditEntries(t, models.AuditImpersonate)
	if len(entries) != 1 || entries[0].Username != "admin" || entries[0].Target != "alice" {
		t.Fatalf("unexpected impersonation entries: %+v", entries)
	}
	// What happens next is recorded against alice, noting who is acting
	client.Get(ctx.adminServer.URL + "/logout")
	logouts := auditEntries(t, models.AuditLogout)
	if len(logouts) != 1 || logouts[0].Username != "alice" || !strings.Contains(logouts[0].Details, "impersonated by admin") {
		t.Fatalf("unexpected logout entries: %+v", logouts)
	}
}

func TestAuditLogPage(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	client := newJarClient(t, ctx)
	postForm(t, ctx, client, "/login", url.Values{"username": {"admin"}, "password": {"gophish"}})
	resp, err := client.Get(ctx.adminServer.URL + "/audit_log")
	if err != nil {
		t.Fatalf("error requesting /audit_log: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status for /audit_log: %d", resp.StatusCode)
	}
}
