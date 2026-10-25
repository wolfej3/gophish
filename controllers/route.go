package controllers

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/gophish/gophish/auth"
	"github.com/gophish/gophish/config"
	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/controllers/api"
	"github.com/gophish/gophish/library"
	log "github.com/gophish/gophish/logger"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/middleware/ratelimit"
	"github.com/gophish/gophish/models"
	"github.com/gophish/gophish/static"
	templatefs "github.com/gophish/gophish/templates"
	"github.com/gophish/gophish/util"
	"github.com/gophish/gophish/worker"
	"github.com/gorilla/csrf"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

// AdminServerOption is a functional option that is used to configure the
// admin server
type AdminServerOption func(*AdminServer)

// AdminServer is an HTTP server that implements the administrative Gophish
// handlers, including the dashboard and REST API.
type AdminServer struct {
	server  *http.Server
	worker  worker.Worker
	config  config.AdminServer
	limiter *ratelimit.PostLimiter
	oidc    *oidcClient
	saml    *samlClient
	library *library.Library
}

var defaultTLSConfig = &tls.Config{
	PreferServerCipherSuites: true,
	CurvePreferences: []tls.CurveID{
		tls.X25519,
		tls.CurveP256,
	},
	MinVersion: tls.VersionTLS12,
	CipherSuites: []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,

		// Kept for backwards compatibility with some clients
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	},
}

// WithWorker is an option that sets the background worker.
func WithWorker(w worker.Worker) AdminServerOption {
	return func(as *AdminServer) {
		as.worker = w
	}
}

// WithLimiter is an option that sets the rate limiter for logins and other
// sensitive requests.
func WithLimiter(limiter *ratelimit.PostLimiter) AdminServerOption {
	return func(as *AdminServer) {
		as.limiter = limiter
	}
}

// NewAdminServer returns a new instance of the AdminServer with the
// provided config and options applied.
func NewAdminServer(config config.AdminServer, options ...AdminServerOption) *AdminServer {
	defaultWorker, _ := worker.New(worker.WithDirectoryConnectors(config.Directory))
	defaultServer := &http.Server{
		ReadTimeout: 10 * time.Second,
		Addr:        config.ListenURL,
	}
	defaultLimiter := ratelimit.NewPostLimiter()
	as := &AdminServer{
		worker:  defaultWorker,
		server:  defaultServer,
		limiter: defaultLimiter,
		config:  config,
		library: library.New(config.TemplateLibrary),
	}
	if as.library != nil {
		// Clone/pull in the background so the first page load isn't blocked on
		// the network; List falls back to an on-demand clone if this is slow.
		go func() {
			if err := as.library.Sync(); err != nil {
				log.Errorf("template library: initial sync failed: %v", err)
			}
		}()
	}
	if config.OIDC != nil {
		client, err := newOIDCClient(*config.OIDC)
		if err != nil {
			log.Errorf("single sign-on is disabled: %v", err)
		} else {
			as.oidc = client
		}
	}
	if config.SAML != nil {
		client, err := newSAMLClient(*config.SAML)
		if err != nil {
			log.Errorf("SAML single sign-on is disabled: %v", err)
		} else {
			as.saml = client
		}
	}
	for _, opt := range options {
		opt(as)
	}
	as.registerRoutes()
	return as
}

// Start launches the admin server, listening on the configured address.
func (as *AdminServer) Start() {
	if as.worker != nil {
		go as.worker.Start()
	}
	if as.config.UseTLS {
		// Only support TLS 1.2 and above - ref #1691, #1689
		as.server.TLSConfig = defaultTLSConfig
		err := util.CheckAndCreateSSL(as.config.CertPath, as.config.KeyPath)
		if err != nil {
			log.Fatal(err)
		}
		log.Infof("Starting admin server at https://%s", as.config.ListenURL)
		log.Fatal(as.server.ListenAndServeTLS(as.config.CertPath, as.config.KeyPath))
	}
	// If TLS isn't configured, just listen on HTTP. Browsers must still reach
	// the admin server over HTTPS (via a TLS-terminating proxy), since CSRF
	// protection rejects form submissions from plain HTTP pages.
	log.Warn("The admin server is running without TLS. Logins and other form submissions will fail unless it's served through a TLS-terminating proxy. Set \"use_tls\": true in the admin_server config to use Gophish's built-in TLS.")
	log.Infof("Starting admin server at http://%s", as.config.ListenURL)
	log.Fatal(as.server.ListenAndServe())
}

// Shutdown attempts to gracefully shutdown the server.
func (as *AdminServer) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	return as.server.Shutdown(ctx)
}

// SetupAdminRoutes creates the routes for handling requests to the web interface.
// This function returns an http.Handler to be used in http.ListenAndServe().
func (as *AdminServer) registerRoutes() {
	router := mux.NewRouter()
	// Base Front-end routes
	router.HandleFunc("/", mid.Use(as.Base, mid.RequireLogin))
	router.HandleFunc("/login", mid.Use(as.Login, as.limiter.Limit))
	router.HandleFunc("/login/2fa", mid.Use(as.LoginSecondFactor, as.limiter.Limit))
	router.HandleFunc("/login/oidc", as.OIDCLogin)
	router.HandleFunc("/login/oidc/callback", as.OIDCCallback)
	router.HandleFunc("/login/saml", as.SAMLLogin)
	router.HandleFunc("/saml/acs", as.SAMLACS)
	router.HandleFunc("/saml/metadata", as.SAMLMetadata)
	router.HandleFunc("/logout", mid.Use(as.Logout, mid.RequireLogin))
	router.HandleFunc("/reset_password", mid.Use(as.ResetPassword, mid.RequireLogin))
	router.HandleFunc("/campaigns", mid.Use(as.Campaigns, mid.RequireLogin))
	router.HandleFunc("/schedules", mid.Use(as.Schedules, mid.RequireLogin))
	router.HandleFunc("/people", mid.Use(as.People, mid.RequireLogin))
	router.HandleFunc("/campaigns/{id:[0-9]+}", mid.Use(as.CampaignID, mid.RequireLogin))
	router.HandleFunc("/templates", mid.Use(as.Templates, mid.RequireLogin))
	router.HandleFunc("/template_library", mid.Use(as.TemplateLibrary, mid.RequireLogin))
	router.HandleFunc("/groups", mid.Use(as.Groups, mid.RequireLogin))
	router.HandleFunc("/landing_pages", mid.Use(as.LandingPages, mid.RequireLogin))
	router.HandleFunc("/sending_profiles", mid.Use(as.SendingProfiles, mid.RequireLogin))
	router.HandleFunc("/settings", mid.Use(as.Settings, mid.RequireLogin))
	router.HandleFunc("/settings/2fa/setup", mid.Use(as.SetupTOTP, as.limiter.Limit, mid.RequireLogin))
	router.HandleFunc("/settings/2fa/enable", mid.Use(as.EnableTOTP, as.limiter.Limit, mid.RequireLogin))
	router.HandleFunc("/settings/2fa/disable", mid.Use(as.DisableTOTP, as.limiter.Limit, mid.RequireLogin))
	router.HandleFunc("/settings/2fa/recovery_codes", mid.Use(as.RegenerateRecoveryCodes, as.limiter.Limit, mid.RequireLogin))
	router.HandleFunc("/settings/api_key/reveal", mid.Use(as.APIKeyReveal, as.limiter.Limit, mid.RequireLogin))
	router.HandleFunc("/settings/default_team", mid.Use(as.SetDefaultTeam, mid.RequireLogin))
	router.HandleFunc("/users", mid.Use(as.UserManagement, mid.RequirePermission(models.PermissionModifySystem), mid.RequireLogin))
	router.HandleFunc("/teams", mid.Use(as.TeamManagement, mid.RequirePermission(models.PermissionModifySystem), mid.RequireLogin))
	router.HandleFunc("/tenants", mid.Use(as.TenantManagement, mid.RequirePermission(models.PermissionManageTenants), mid.RequireLogin))
	router.HandleFunc("/webhooks", mid.Use(as.Webhooks, mid.RequirePermission(models.PermissionModifySystem), mid.RequireLogin))
	router.HandleFunc("/audit_log", mid.Use(as.AuditLog, mid.RequirePermission(models.PermissionModifySystem), mid.RequireLogin))
	router.HandleFunc("/impersonate", mid.Use(as.Impersonate, mid.RequirePermission(models.PermissionModifySystem), mid.RequireLogin))
	// Create the API routes
	api := api.NewServer(
		api.WithWorker(as.worker),
		api.WithLimiter(as.limiter),
		api.WithDirectoryConnectors(as.config.Directory),
		api.WithTemplateLibrary(as.library),
	)
	router.PathPrefix("/api/").Handler(api)

	// Setup static file serving
	router.PathPrefix("/").Handler(static.Handler())

	// Setup CSRF Protection
	csrfKey := []byte(as.config.CSRFKey)
	if len(csrfKey) == 0 {
		csrfKey = []byte(auth.GenerateSecureKey(auth.APIKeyLength))
	}
	csrfHandler := csrf.Protect(csrfKey,
		csrf.FieldName("csrf_token"),
		csrf.Secure(as.config.UseTLS),
		csrf.TrustedOrigins(as.config.TrustedOrigins))
	adminHandler := csrfHandler(router)
	// Only send the session cookie over TLS when the admin server uses it
	mid.Store.Options.Secure = as.config.UseTLS
	adminHandler = mid.Use(adminHandler.ServeHTTP, mid.CSRFExceptions, mid.GetContext, mid.ApplySecurityHeaders)

	// Setup GZIP compression
	gzipWrapper, _ := gziphandler.NewGzipLevelHandler(gzip.BestCompression)
	adminHandler = gzipWrapper(adminHandler)

	// Respect X-Forwarded-For and X-Real-IP headers only from trusted
	// reverse proxies, so clients can't spoof their IP.
	trustedProxies, err := config.ParseTrustedProxies(as.config.TrustedProxies)
	if err != nil {
		log.Fatalf("invalid admin_server.trusted_proxies: %v", err)
	}
	adminHandler = trustedProxyHeaders(trustedProxies, adminHandler)

	// Setup logging
	adminHandler = handlers.CombinedLoggingHandler(log.Writer(), adminHandler)
	as.server.Handler = adminHandler
}

type templateParams struct {
	Title         string
	Flashes       []interface{}
	User          models.User
	Token         string
	Version       string
	ModifySystem  bool
	ManageTenants bool
}

// newTemplateParams returns the default template parameters for a user and
// the CSRF token.
func newTemplateParams(r *http.Request) templateParams {
	user := ctx.Get(r, "user").(models.User)
	session := ctx.Get(r, "session").(*sessions.Session)
	modifySystem, _ := user.HasPermission(models.PermissionModifySystem)
	manageTenants, _ := user.HasPermission(models.PermissionManageTenants)
	return templateParams{
		Token:         csrf.Token(r),
		User:          user,
		ModifySystem:  modifySystem,
		ManageTenants: manageTenants,
		Version:       config.Version,
		Flashes:       session.Flashes(),
	}
}

// Base handles the default path and template execution
func (as *AdminServer) Base(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Dashboard"
	getTemplate(w, "dashboard").ExecuteTemplate(w, "base", params)
}

// People shows cross-campaign activity for each recipient
func (as *AdminServer) People(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "People"
	getTemplate(w, "people").ExecuteTemplate(w, "base", params)
}

// Campaigns handles the default path and template execution
func (as *AdminServer) Campaigns(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Campaigns"
	getTemplate(w, "campaigns").ExecuteTemplate(w, "base", params)
}

// Schedules handles the default path and template for recurring campaign schedules
func (as *AdminServer) Schedules(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Schedules"
	getTemplate(w, "schedules").ExecuteTemplate(w, "base", params)
}

// CampaignID handles the default path and template execution
func (as *AdminServer) CampaignID(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Campaign Results"
	getTemplate(w, "campaign_results").ExecuteTemplate(w, "base", params)
}

// Templates handles the default path and template execution
func (as *AdminServer) Templates(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Email Templates"
	getTemplate(w, "templates").ExecuteTemplate(w, "base", params)
}

// TemplateLibrary handles the default path and template for the git-backed
// template library browser.
func (as *AdminServer) TemplateLibrary(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Template Library"
	getTemplate(w, "template_library").ExecuteTemplate(w, "base", params)
}

// Groups handles the default path and template execution
func (as *AdminServer) Groups(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Users & Groups"
	getTemplate(w, "groups").ExecuteTemplate(w, "base", params)
}

// LandingPages handles the default path and template execution
func (as *AdminServer) LandingPages(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Landing Pages"
	getTemplate(w, "landing_pages").ExecuteTemplate(w, "base", params)
}

// SendingProfiles handles the default path and template execution
func (as *AdminServer) SendingProfiles(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Sending Profiles"
	getTemplate(w, "sending_profiles").ExecuteTemplate(w, "base", params)
}

// Settings handles the changing of settings
func (as *AdminServer) Settings(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET":
		params := newTemplateParams(r)
		params.Title = "Settings"
		session := ctx.Get(r, "session").(*sessions.Session)
		session.Save(r, w)
		getTemplate(w, "settings").ExecuteTemplate(w, "base", params)
	case r.Method == "POST":
		u := ctx.Get(r, "user").(models.User)
		currentPw := r.FormValue("current_password")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("confirm_new_password")
		// Check the current password
		err := auth.ValidatePassword(currentPw, u.Hash)
		msg := models.Response{Success: true, Message: "Settings Updated Successfully"}
		if err != nil {
			msg.Message = err.Error()
			msg.Success = false
			api.JSONResponse(w, msg, http.StatusBadRequest)
			return
		}
		newHash, err := auth.ValidatePasswordChange(u.Hash, newPassword, confirmPassword)
		if err != nil {
			msg.Message = err.Error()
			msg.Success = false
			api.JSONResponse(w, msg, http.StatusBadRequest)
			return
		}
		u.Hash = string(newHash)
		if err = models.PutUser(&u); err != nil {
			msg.Message = err.Error()
			msg.Success = false
			api.JSONResponse(w, msg, http.StatusInternalServerError)
			return
		}
		// Sign out any other sessions using the old password
		if err = mid.RevokeOtherSessions(r, u.Id); err != nil {
			log.Error(err)
		}
		mid.Audit(r, &u, "", models.AuditPasswordChanged, "", "settings")
		api.JSONResponse(w, msg, http.StatusOK)
	}
}

// SetDefaultTeam lets a user choose which of their teams they land in by
// default. It only accepts a team the user can actually access.
func (as *AdminServer) SetDefaultTeam(w http.ResponseWriter, r *http.Request) {
	u := ctx.Get(r, "user").(models.User)
	tid, _ := strconv.ParseInt(r.FormValue("default_team_id"), 10, 64)
	if tid != 0 {
		teams, err := models.GetUserTeams(u)
		if err != nil {
			api.JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		allowed := false
		for _, t := range teams {
			if t.Id == tid {
				allowed = true
				break
			}
		}
		if !allowed {
			api.JSONResponse(w, models.Response{Success: false, Message: "You can't set a default team you don't have access to"}, http.StatusBadRequest)
			return
		}
	}
	u.DefaultTeamId = tid
	if err := models.PutUser(&u); err != nil {
		api.JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
		return
	}
	api.JSONResponse(w, models.Response{Success: true, Message: "Default team updated"}, http.StatusOK)
}

// UserManagement is an admin-only handler that allows for the registration
// and management of user accounts within Gophish.
func (as *AdminServer) UserManagement(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "User Management"
	getTemplate(w, "users").ExecuteTemplate(w, "base", params)
}

func (as *AdminServer) nextOrIndex(w http.ResponseWriter, r *http.Request) {
	redirectNext(w, r, r.FormValue("next"))
}

// safeNext returns the local path in next, or "/" if there isn't one. Only
// the path is kept, so that next can't send the user to another site.
func safeNext(next string) string {
	url, err := url.Parse(next)
	if err == nil {
		path := url.EscapedPath()
		if path != "" {
			return "/" + strings.TrimLeft(path, "/")
		}
	}
	return "/"
}

// redirectNext redirects the user to the local path in next, or to the
// dashboard.
func redirectNext(w http.ResponseWriter, r *http.Request, next string) {
	http.Redirect(w, r, safeNext(next), http.StatusFound)
}

// loginParams are the template parameters for the login page.
type loginParams struct {
	User       models.User
	Title      string
	Flashes    []interface{}
	Token      string
	SSOButtons []ssoButton
	Next       string
	// PasswordLoginDisabled hides the username and password form
	PasswordLoginDisabled bool
}

func (as *AdminServer) newLoginParams(r *http.Request) loginParams {
	return loginParams{
		Title:      "Login",
		Token:      csrf.Token(r),
		SSOButtons: as.ssoButtons(),
		Next:       r.FormValue("next"),

		PasswordLoginDisabled: as.passwordLoginDisabled(),
	}
}

func (as *AdminServer) handleInvalidLogin(w http.ResponseWriter, r *http.Request, message string) {
	session := ctx.Get(r, "session").(*sessions.Session)
	Flash(w, r, "danger", message)
	params := as.newLoginParams(r)
	params.Flashes = session.Flashes()
	session.Save(r, w)
	templates := template.New("template")
	_, err := templates.ParseFS(templatefs.FS, "login.html", "flashes.html")
	if err != nil {
		log.Error(err)
	}
	// w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	template.Must(templates, err).ExecuteTemplate(w, "base", params)
}

// Webhooks is an admin-only handler that handles webhooks
func (as *AdminServer) Webhooks(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Webhooks"
	getTemplate(w, "webhooks").ExecuteTemplate(w, "base", params)
}

// TeamManagement is an admin-only handler for managing teams and their members
func (as *AdminServer) TeamManagement(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Teams"
	getTemplate(w, "teams").ExecuteTemplate(w, "base", params)
}

// TenantManagement is a provider-admin-only handler for managing tenants
func (as *AdminServer) TenantManagement(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Tenants"
	getTemplate(w, "tenants").ExecuteTemplate(w, "base", params)
}

// AuditLog is an admin-only handler that shows the audit log
func (as *AdminServer) AuditLog(w http.ResponseWriter, r *http.Request) {
	params := newTemplateParams(r)
	params.Title = "Audit Log"
	getTemplate(w, "audit_log").ExecuteTemplate(w, "base", params)
}

// Impersonate allows an admin to login to a user account without needing the password
func (as *AdminServer) Impersonate(w http.ResponseWriter, r *http.Request) {

	if r.Method == "POST" {
		username := r.FormValue("username")
		u, err := models.GetUserByUsername(username)
		if err != nil {
			log.Error(err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Authorize the impersonation. modify_system (held by every tenant
		// admin) is enough to reach this handler, so without a tenant/role
		// check a tenant admin could impersonate the provider admin — who
		// spans every tenant — and take over the whole system. A provider
		// admin may impersonate anyone; anyone else may impersonate only
		// users within their own tenant, and never a provider admin.
		actor := ctx.Get(r, "user").(models.User)
		if isProvider, _ := actor.HasPermission(models.PermissionManageTenants); !isProvider {
			if u.Role.Slug == models.RoleProviderAdmin || u.TenantId != actor.TenantId {
				log.Warnf("user %q tried to impersonate %q outside their authority", actor.Username, u.Username)
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
		}
		session := ctx.Get(r, "session").(*sessions.Session)
		// Issue a new session token for the impersonated account
		err = mid.RegenerateSession(session)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		mid.AuditUser(r, models.AuditImpersonate, u.Username, "")
		// Remember who is really acting, for the audit log. If they're
		// already impersonating someone, keep the original account.
		if _, ok := session.Values[mid.ImpersonatorKey]; !ok {
			session.Values[mid.ImpersonatorKey] = ctx.Get(r, "user").(models.User).Username
		}
		session.Values["id"] = u.Id
		session.Save(r, w)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// Login handles the authentication flow for a user. If credentials are valid,
// a session is created
func (as *AdminServer) Login(w http.ResponseWriter, r *http.Request) {
	params := as.newLoginParams(r)
	session := ctx.Get(r, "session").(*sessions.Session)
	switch {
	case r.Method == "GET":
		params.Flashes = session.Flashes()
		session.Save(r, w)
		templates := template.New("template")
		_, err := templates.ParseFS(templatefs.FS, "login.html", "flashes.html")
		if err != nil {
			log.Error(err)
		}
		template.Must(templates, err).ExecuteTemplate(w, "base", params)
	case r.Method == "POST":
		// Find the user with the provided username
		username, password := r.FormValue("username"), r.FormValue("password")
		if as.passwordLoginDisabled() {
			mid.Audit(r, nil, username, models.AuditLoginFailed, "", "password login is disabled")
			as.handleInvalidLogin(w, r, "Password login is disabled. Please use single sign-on.")
			return
		}
		u, err := models.GetUserByUsername(username)
		if err != nil {
			log.Error(err)
			// Spend the same time as a real bcrypt comparison so an
			// attacker can't tell existing usernames apart by timing.
			auth.CompareDummyPassword(password)
			mid.Audit(r, nil, username, models.AuditLoginFailed, "", "unknown username")
			as.handleInvalidLogin(w, r, "Invalid Username/Password")
			return
		}
		// Validate the user's password
		err = auth.ValidatePassword(password, u.Hash)
		if err != nil {
			log.Error(err)
			mid.Audit(r, &u, "", models.AuditLoginFailed, "", "invalid password")
			as.handleInvalidLogin(w, r, "Invalid Username/Password")
			return
		}
		if u.AccountLocked {
			mid.Audit(r, &u, "", models.AuditLoginFailed, "", "account locked")
			as.handleInvalidLogin(w, r, "Account Locked")
			return
		}
		// Users with two-factor authentication enabled have to provide a
		// code before they're logged in
		if u.TOTPEnabled {
			as.startSecondFactor(w, r, u, r.FormValue("next"), "password")
			return
		}
		as.completeLogin(w, r, u, r.FormValue("next"), "password")
	}
}

// completeLogin logs in the user with a new session and redirects them to the
// page in next, or to the dashboard. method describes how they signed in,
// for the audit log.
func (as *AdminServer) completeLogin(w http.ResponseWriter, r *http.Request, u models.User, next, method string) {
	session := ctx.Get(r, "session").(*sessions.Session)
	u.LastLogin = time.Now().UTC()
	err := models.PutUser(&u)
	if err != nil {
		log.Error(err)
	}
	// If we've logged in, save the session under a new token and redirect
	// to the dashboard
	err = mid.RegenerateSession(session)
	if err != nil {
		log.Error(err)
		as.handleInvalidLogin(w, r, "Error creating session")
		return
	}
	clearSecondFactor(session)
	session.Values["id"] = u.Id
	session.Save(r, w)
	mid.Audit(r, &u, "", models.AuditLogin, "", method)
	err = models.DeleteExpiredSessions()
	if err != nil {
		log.Error(err)
	}
	redirectNext(w, r, next)
}

// Logout destroys the current user session
func (as *AdminServer) Logout(w http.ResponseWriter, r *http.Request) {
	session := ctx.Get(r, "session").(*sessions.Session)
	mid.AuditUser(r, models.AuditLogout, "", "")
	// Delete the logged in session so its token can't be reused, keeping only
	// the logout message for the login page
	err := mid.RegenerateSession(session)
	if err != nil {
		log.Error(err)
	}
	session.Values = make(map[interface{}]interface{})
	Flash(w, r, "success", "You have successfully logged out")
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ResetPassword handles the password reset flow when a password change is
// required either by the Gophish system or an administrator.
//
// This handler is meant to be used when a user is required to reset their
// password, not just when they want to.
//
// This is an important distinction since in this handler we don't require
// the user to re-enter their current password, as opposed to the flow
// through the settings handler.
//
// To that end, if the user doesn't require a password change, we will
// redirect them to the settings page.
func (as *AdminServer) ResetPassword(w http.ResponseWriter, r *http.Request) {
	u := ctx.Get(r, "user").(models.User)
	session := ctx.Get(r, "session").(*sessions.Session)
	if !u.PasswordChangeRequired {
		Flash(w, r, "info", "Please reset your password through the settings page")
		session.Save(r, w)
		http.Redirect(w, r, "/settings", http.StatusTemporaryRedirect)
		return
	}
	params := newTemplateParams(r)
	params.Title = "Reset Password"
	switch {
	case r.Method == http.MethodGet:
		params.Flashes = session.Flashes()
		session.Save(r, w)
		getTemplate(w, "reset_password").ExecuteTemplate(w, "base", params)
		return
	case r.Method == http.MethodPost:
		newPassword := r.FormValue("password")
		confirmPassword := r.FormValue("confirm_password")
		newHash, err := auth.ValidatePasswordChange(u.Hash, newPassword, confirmPassword)
		if err != nil {
			Flash(w, r, "danger", err.Error())
			params.Flashes = session.Flashes()
			session.Save(r, w)
			w.WriteHeader(http.StatusBadRequest)
			getTemplate(w, "reset_password").ExecuteTemplate(w, "base", params)
			return
		}
		u.PasswordChangeRequired = false
		u.Hash = newHash
		if err = models.PutUser(&u); err != nil {
			Flash(w, r, "danger", err.Error())
			params.Flashes = session.Flashes()
			session.Save(r, w)
			w.WriteHeader(http.StatusInternalServerError)
			getTemplate(w, "reset_password").ExecuteTemplate(w, "base", params)
			return
		}
		// Sign out any other sessions using the old password
		if err = mid.RevokeOtherSessions(r, u.Id); err != nil {
			log.Error(err)
		}
		mid.Audit(r, &u, "", models.AuditPasswordChanged, "", "required reset")
		// TODO: We probably want to flash a message here that the password was
		// changed successfully. The problem is that when the user resets their
		// password on first use, they will see two flashes on the dashboard-
		// one for their password reset, and one for the "no campaigns created".
		//
		// The solution to this is to revamp the empty page to be more useful,
		// like a wizard or something.
		as.nextOrIndex(w, r)
	}
}

// TODO: Make this execute the template, too
func getTemplate(w http.ResponseWriter, tmpl string) *template.Template {
	templates := template.New("template")
	_, err := templates.ParseFS(templatefs.FS, "base.html", "nav.html", tmpl+".html", "flashes.html")
	if err != nil {
		log.Error(err)
	}
	return template.Must(templates, err)
}

// Flash handles the rendering flash messages
func Flash(w http.ResponseWriter, r *http.Request, t string, m string) {
	session := ctx.Get(r, "session").(*sessions.Session)
	session.AddFlash(models.Flash{
		Type:    t,
		Message: m,
	})
}
