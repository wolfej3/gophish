package controllers

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/config"
	ctx "github.com/gophish/gophish/context"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

// Session keys used while a user is signing in through the identity
// provider.
const (
	oidcStateKey    = "oidc_state"
	oidcNonceKey    = "oidc_nonce"
	oidcVerifierKey = "oidc_verifier"
	oidcNextKey     = "oidc_next"
)

// oidcTimeout bounds each request we make to the identity provider.
const oidcTimeout = 15 * time.Second

// errSSOLogin is shown to users when single sign-on fails. The reason is
// logged rather than shown, since it may reveal details about accounts.
var errSSOLogin = errors.New("Single sign-on failed. Please contact your administrator.")

// oidcClient signs users in to the admin server through an OpenID Connect
// identity provider. The provider's configuration is discovered the first
// time someone signs in, so the admin server can start while it's down.
type oidcClient struct {
	conf config.OIDC

	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// newOIDCClient returns a client for the configured identity provider, with
// defaults applied, or an error if the configuration is incomplete.
func newOIDCClient(conf config.OIDC) (*oidcClient, error) {
	switch {
	case conf.Issuer == "":
		return nil, errors.New("oidc: issuer is required")
	case conf.ClientID == "":
		return nil, errors.New("oidc: client_id is required")
	case conf.RedirectURL == "":
		return nil, errors.New("oidc: redirect_url is required")
	}
	if len(conf.Scopes) == 0 {
		conf.Scopes = []string{"email", "profile"}
	}
	if conf.UsernameClaim == "" {
		conf.UsernameClaim = "email"
	}
	if conf.ButtonLabel == "" {
		conf.ButtonLabel = "Sign in with SSO"
	}
	if conf.DefaultRole == "" {
		conf.DefaultRole = models.RoleUser
	}
	return &oidcClient{conf: conf}, nil
}

// setup discovers the identity provider's configuration if we haven't yet.
func (c *oidcClient) setup(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, c.conf.Issuer)
	if err != nil {
		return err
	}
	c.provider = provider
	c.oauth = &oauth2.Config{
		ClientID:     c.conf.ClientID,
		ClientSecret: c.conf.ClientSecret,
		RedirectURL:  c.conf.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       append([]string{oidc.ScopeOpenID}, c.conf.Scopes...),
	}
	c.verifier = provider.Verifier(&oidc.Config{ClientID: c.conf.ClientID})
	return nil
}

// ssoLabel returns the label for the single sign-on button on the login page,
// or an empty string if single sign-on isn't configured.
func (as *AdminServer) ssoLabel() string {
	if as.oidc == nil {
		return ""
	}
	return as.oidc.conf.ButtonLabel
}

// passwordLoginDisabled returns whether users must sign in through single
// sign-on instead of with a password.
func (as *AdminServer) passwordLoginDisabled() bool {
	return as.oidc != nil && as.oidc.conf.DisablePasswordLogin
}

// ssoError logs why single sign-on failed and sends the user back to the
// login page.
func (as *AdminServer) ssoError(w http.ResponseWriter, r *http.Request, err error) {
	log.Errorf("single sign-on failed: %v", err)
	session := ctx.Get(r, "session").(*sessions.Session)
	for _, k := range []string{oidcStateKey, oidcNonceKey, oidcVerifierKey, oidcNextKey} {
		delete(session.Values, k)
	}
	Flash(w, r, "danger", errSSOLogin.Error())
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// OIDCLogin sends the user to the identity provider to sign in.
func (as *AdminServer) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	if as.oidc == nil {
		http.NotFound(w, r)
		return
	}
	reqCtx, cancel := context.WithTimeout(r.Context(), oidcTimeout)
	defer cancel()
	if err := as.oidc.setup(reqCtx); err != nil {
		as.ssoError(w, r, fmt.Errorf("discovering identity provider: %w", err))
		return
	}
	state := auth.GenerateSecureKey(32)
	nonce := auth.GenerateSecureKey(32)
	verifier := oauth2.GenerateVerifier()
	session := ctx.Get(r, "session").(*sessions.Session)
	session.Values[oidcStateKey] = state
	session.Values[oidcNonceKey] = nonce
	session.Values[oidcVerifierKey] = verifier
	session.Values[oidcNextKey] = safeNext(r.FormValue("next"))
	session.Save(r, w)
	http.Redirect(w, r, as.oidc.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// OIDCCallback handles the identity provider sending the user back after
// they've signed in, and logs them in to the matching Gophish account.
func (as *AdminServer) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if as.oidc == nil {
		http.NotFound(w, r)
		return
	}
	session := ctx.Get(r, "session").(*sessions.Session)
	state, _ := session.Values[oidcStateKey].(string)
	nonce, _ := session.Values[oidcNonceKey].(string)
	verifier, _ := session.Values[oidcVerifierKey].(string)
	next, _ := session.Values[oidcNextKey].(string)
	// Each sign in attempt can only be completed once
	for _, k := range []string{oidcStateKey, oidcNonceKey, oidcVerifierKey, oidcNextKey} {
		delete(session.Values, k)
	}
	session.Save(r, w)
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(r.FormValue("state"))) != 1 {
		as.ssoError(w, r, errors.New("state doesn't match"))
		return
	}
	if e := r.FormValue("error"); e != "" {
		as.ssoError(w, r, fmt.Errorf("identity provider returned %s: %s", e, r.FormValue("error_description")))
		return
	}
	reqCtx, cancel := context.WithTimeout(r.Context(), oidcTimeout)
	defer cancel()
	if err := as.oidc.setup(reqCtx); err != nil {
		as.ssoError(w, r, fmt.Errorf("discovering identity provider: %w", err))
		return
	}
	token, err := as.oidc.oauth.Exchange(reqCtx, r.FormValue("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		as.ssoError(w, r, fmt.Errorf("exchanging code: %w", err))
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		as.ssoError(w, r, errors.New("no id_token in token response"))
		return
	}
	idToken, err := as.oidc.verifier.Verify(reqCtx, rawIDToken)
	if err != nil {
		as.ssoError(w, r, fmt.Errorf("verifying id_token: %w", err))
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		as.ssoError(w, r, errors.New("nonce doesn't match"))
		return
	}
	claims := map[string]interface{}{}
	if err := idToken.Claims(&claims); err != nil {
		as.ssoError(w, r, fmt.Errorf("parsing claims: %w", err))
		return
	}
	u, err := as.oidcUser(idToken, claims)
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
	if u.TOTPEnabled {
		as.startSecondFactor(w, r, u, next)
		return
	}
	as.completeLogin(w, r, u, next)
}

// oidcUser returns the Gophish account for a verified ID token. Accounts are
// found by the identity they're linked to. An account that isn't linked yet
// is linked the first time its user signs in, matched on username. If there
// is no matching account, one is created if the configuration allows it.
func (as *AdminServer) oidcUser(idToken *oidc.IDToken, claims map[string]interface{}) (models.User, error) {
	conf := as.oidc.conf
	identity := idToken.Issuer + " " + idToken.Subject
	u, err := models.GetUserByOIDCIdentity(identity)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return u, err
	}
	username, _ := claims[conf.UsernameClaim].(string)
	if username == "" {
		return u, fmt.Errorf("id_token has no %q claim", conf.UsernameClaim)
	}
	// Don't trust an email address the identity provider says it hasn't
	// verified, since anyone could have claimed it.
	if conf.UsernameClaim == "email" {
		if verified, ok := claims["email_verified"].(bool); ok && !verified {
			return u, fmt.Errorf("email %q isn't verified", username)
		}
	}
	if len(conf.AllowedDomains) > 0 && !inAllowedDomain(username, conf.AllowedDomains) {
		return u, fmt.Errorf("%q isn't in an allowed domain", username)
	}
	u, err = models.GetUserByUsername(username)
	switch {
	case err == nil:
		if u.OIDCIdentity != "" {
			return u, fmt.Errorf("account %q is already linked to another identity", username)
		}
		if u.AccountLocked {
			return u, fmt.Errorf("account %q is locked", username)
		}
		u.OIDCIdentity = identity
		if err := models.PutUser(&u); err != nil {
			return u, err
		}
		log.Infof("linked account %q to single sign-on identity %q", username, identity)
		return u, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return u, err
	case !conf.AutoCreateUsers:
		return u, fmt.Errorf("no account for %q", username)
	}
	role, err := models.GetRoleBySlug(conf.DefaultRole)
	if err != nil {
		return u, fmt.Errorf("getting role %q: %w", conf.DefaultRole, err)
	}
	// The account signs in only through single sign-on, so its password is
	// a random one nobody knows.
	hash, err := auth.GeneratePasswordHash(auth.GenerateSecureKey(32))
	if err != nil {
		return u, err
	}
	u = models.User{
		Username:     username,
		Hash:         hash,
		ApiKey:       auth.GenerateSecureKey(auth.APIKeyLength),
		Role:         role,
		RoleID:       role.ID,
		OIDCIdentity: identity,
	}
	if err := models.PutUser(&u); err != nil {
		return u, err
	}
	log.Infof("created account %q for single sign-on identity %q", username, identity)
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
