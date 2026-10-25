package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gophish/gophish/auth"
	ctx "github.com/gophish/gophish/context"
	log "github.com/gophish/gophish/logger"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/sessions"
	"gorm.io/gorm"
)

// errSSOLogin is shown to users when single sign-on fails. The reason is
// logged rather than shown, since it may reveal details about accounts.
var errSSOLogin = errors.New("Single sign-on failed. Please contact your administrator.")

// ssoButton is a single sign-on button on the login page.
type ssoButton struct {
	Label string
	URL   string
}

// ssoButtons returns the single sign-on buttons for the login page.
func (as *AdminServer) ssoButtons() []ssoButton {
	buttons := []ssoButton{}
	if as.oidc != nil {
		buttons = append(buttons, ssoButton{Label: as.oidc.conf.ButtonLabel, URL: "/login/oidc"})
	}
	if as.saml != nil {
		buttons = append(buttons, ssoButton{Label: as.saml.conf.ButtonLabel, URL: "/login/saml"})
	}
	return buttons
}

// passwordLoginDisabled returns whether users must sign in through single
// sign-on instead of with a password.
func (as *AdminServer) passwordLoginDisabled() bool {
	return (as.oidc != nil && as.oidc.conf.DisablePasswordLogin) ||
		(as.saml != nil && as.saml.conf.DisablePasswordLogin)
}

// ssoError logs why single sign-on failed and sends the user back to the
// login page.
func (as *AdminServer) ssoError(w http.ResponseWriter, r *http.Request, err error) {
	log.Errorf("single sign-on failed: %v", err)
	mid.Audit(r, nil, "", models.AuditLoginFailed, "", "single sign-on: "+err.Error())
	session := ctx.Get(r, "session").(*sessions.Session)
	Flash(w, r, "danger", errSSOLogin.Error())
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ssoIdentity is who the identity provider says signed in.
type ssoIdentity struct {
	// Provider is models.SSOProviderOIDC or models.SSOProviderSAML.
	Provider string
	// Identity identifies the person at the identity provider, and doesn't
	// change if their username does.
	Identity string
	// Username is matched against Gophish usernames.
	Username string
}

// ssoAccounts configures how single sign-on identities are matched to
// Gophish accounts.
type ssoAccounts struct {
	AutoCreateUsers bool
	DefaultRole     string
	AllowedDomains  []string
}

// finishSSOLogin logs in the account for the identity the identity provider
// verified, asking for a two-factor code first if the account has it on.
func (as *AdminServer) finishSSOLogin(w http.ResponseWriter, r *http.Request, id ssoIdentity, accounts ssoAccounts, next string) {
	u, err := ssoUser(id, accounts)
	if err != nil {
		as.ssoError(w, r, err)
		return
	}
	if u.AccountLocked {
		as.ssoError(w, r, fmt.Errorf("account %q is locked", u.Username))
		return
	}
	// There's no point making someone choose a new password they can't use
	if as.passwordLoginDisabled() && u.PasswordChangeRequired {
		u.PasswordChangeRequired = false
		if err := models.PutUser(&u); err != nil {
			as.ssoError(w, r, err)
			return
		}
	}
	method := "single sign-on (" + id.Provider + ")"
	if u.TOTPEnabled {
		as.startSecondFactor(w, r, u, next, method)
		return
	}
	as.completeLogin(w, r, u, next, method)
}

// ssoUser returns the Gophish account for an identity the identity provider
// verified. Accounts are found by the identity they're linked to. An account
// that isn't linked yet is linked the first time its user signs in, matched
// on username. If there is no matching account, one is created if the
// configuration allows it.
func ssoUser(id ssoIdentity, accounts ssoAccounts) (models.User, error) {
	u, err := models.GetUserBySSOIdentity(id.Provider, id.Identity)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return u, err
	}
	username := id.Username
	if username == "" {
		return u, errors.New("identity provider didn't send a username")
	}
	if len(accounts.AllowedDomains) > 0 && !inAllowedDomain(username, accounts.AllowedDomains) {
		return u, fmt.Errorf("%q isn't in an allowed domain", username)
	}
	u, err = models.GetUserByUsername(username)
	switch {
	case err == nil:
		if u.SSOIdentity(id.Provider) != "" {
			return u, fmt.Errorf("account %q is already linked to another identity", username)
		}
		if u.AccountLocked {
			return u, fmt.Errorf("account %q is locked", username)
		}
		u.SetSSOIdentity(id.Provider, id.Identity)
		if err := models.PutUser(&u); err != nil {
			return u, err
		}
		log.Infof("linked account %q to single sign-on identity %q", username, id.Identity)
		return u, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return u, err
	case !accounts.AutoCreateUsers:
		return u, fmt.Errorf("no account for %q", username)
	}
	role, err := models.GetRoleBySlug(accounts.DefaultRole)
	if err != nil {
		return u, fmt.Errorf("getting role %q: %w", accounts.DefaultRole, err)
	}
	// Single sign-on is operator-level config shared across tenants, so an
	// auto-provisioned account lands in the Default tenant.
	tenant, err := models.GetDefaultTenant()
	if err != nil {
		return u, err
	}
	// The account signs in only through single sign-on, so its password is
	// a random one nobody knows.
	hash, err := auth.GeneratePasswordHash(auth.GenerateSecureKey(32))
	if err != nil {
		return u, err
	}
	u = models.User{
		Username: username,
		Hash:     hash,
		ApiKey:   auth.GenerateSecureKey(auth.APIKeyLength),
		Role:     role,
		RoleID:   role.ID,
		TenantId: tenant.Id,
	}
	u.SetSSOIdentity(id.Provider, id.Identity)
	if err := models.PutUser(&u); err != nil {
		return u, err
	}
	// Give the account a private personal team. Their default team is left
	// unset until they choose one.
	if _, err := models.EnsurePersonalTeam(u); err != nil {
		return u, err
	}
	log.Infof("created account %q for single sign-on identity %q", username, id.Identity)
	return u, nil
}

// inAllowedDomain returns whether the email address is in one of the
// domains.
func inAllowedDomain(email string, domains []string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, d := range domains {
		if strings.EqualFold(domain, strings.TrimPrefix(d, "@")) {
			return true
		}
	}
	return false
}
