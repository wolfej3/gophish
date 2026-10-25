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

// createImpersonationUser makes a user who can sign in with a password, in a
// given role and tenant, with their own personal team.
func createImpersonationUser(t *testing.T, username, password, roleSlug string, tenantID int64) models.User {
	t.Helper()
	role, err := models.GetRoleBySlug(roleSlug)
	if err != nil {
		t.Fatalf("error getting role %q: %v", roleSlug, err)
	}
	hash, err := auth.GeneratePasswordHash(password)
	if err != nil {
		t.Fatalf("error hashing password: %v", err)
	}
	u := models.User{
		Username: username,
		Hash:     hash,
		ApiKey:   auth.GenerateSecureKey(auth.APIKeyLength),
		Role:     role,
		RoleID:   role.ID,
		TenantId: tenantID,
	}
	if err := models.PutUser(&u); err != nil {
		t.Fatalf("error creating user %q: %v", username, err)
	}
	if _, err := models.EnsurePersonalTeam(u); err != nil {
		t.Fatalf("error creating personal team for %q: %v", username, err)
	}
	return u
}

// TestImpersonateAuthorization guards against privilege escalation through
// /impersonate: a tenant admin (who has modify_system) must not be able to
// impersonate the provider admin or a user in another tenant.
func TestImpersonateAuthorization(t *testing.T) {
	ctx := setupTwoFactorTest(t)
	defer tearDown(t, ctx)
	// User 1 ("admin") is the provider admin, in the Default tenant.
	admin, err := models.GetUser(1)
	if err != nil {
		t.Fatalf("error getting admin user: %v", err)
	}
	// A second tenant with a user of its own.
	other := models.Tenant{Name: "Client A"}
	if err := models.PostTenant(&other); err != nil {
		t.Fatalf("error creating tenant: %v", err)
	}
	const pw = "impersonate-test-pw"
	createImpersonationUser(t, "tadmin", pw, models.RoleAdmin, admin.TenantId)
	createImpersonationUser(t, "peer", "x", models.RoleUser, admin.TenantId)
	createImpersonationUser(t, "outsider", "x", models.RoleUser, other.Id)

	client := newJarClient(t, ctx)
	if resp := postForm(t, ctx, client, "/login", url.Values{"username": {"tadmin"}, "password": {pw}}); resp.StatusCode != http.StatusFound {
		t.Fatalf("tenant admin login: expected 302, got %d", resp.StatusCode)
	}

	cases := []struct {
		name, target string
		want         int
	}{
		{"provider admin is off limits", "admin", http.StatusForbidden},
		{"other tenant is off limits", "outsider", http.StatusForbidden},
		{"own-tenant peer is allowed", "peer", http.StatusFound},
	}
	for _, c := range cases {
		resp := postFormTo(t, ctx, client, "/settings", "/impersonate", url.Values{"username": {c.target}})
		if resp.StatusCode != c.want {
			t.Fatalf("%s: impersonating %q expected %d, got %d", c.name, c.target, c.want, resp.StatusCode)
		}
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
