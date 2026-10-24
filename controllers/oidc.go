package controllers

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
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
	if conf.AutoCreateUsers {
		if conf.DefaultRole == models.RoleAdmin && !conf.AllowAdminAutoCreate {
			return nil, errors.New("auto_create_users with default_role \"admin\" is refused; set allow_admin_auto_create to confirm")
		}
		if len(conf.AllowedDomains) == 0 {
			log.Warn("single sign-on auto_create_users is on with no allowed_domains; anyone who can authenticate at the identity provider gets an account")
		}
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
	username, _ := claims[as.oidc.conf.UsernameClaim].(string)
	if username == "" {
		as.ssoError(w, r, fmt.Errorf("id_token has no %q claim", as.oidc.conf.UsernameClaim))
		return
	}
	// Don't trust an email address the identity provider says it hasn't
	// verified, since anyone could have claimed it.
	if as.oidc.conf.UsernameClaim == "email" {
		if verified, ok := claims["email_verified"].(bool); ok && !verified {
			as.ssoError(w, r, fmt.Errorf("email %q isn't verified", username))
			return
		}
	}
	as.finishSSOLogin(w, r, ssoIdentity{
		Provider: models.SSOProviderOIDC,
		Identity: idToken.Issuer + " " + idToken.Subject,
		Username: username,
	}, as.oidc.accounts(), next)
}

// accounts returns how the configuration matches and creates accounts.
func (c *oidcClient) accounts() ssoAccounts {
	return ssoAccounts{
		AutoCreateUsers: c.conf.AutoCreateUsers,
		DefaultRole:     c.conf.DefaultRole,
		AllowedDomains:  c.conf.AllowedDomains,
	}
}
