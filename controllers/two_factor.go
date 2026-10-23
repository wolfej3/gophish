package controllers

import (
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/gophish/gophish/auth"
	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/controllers/api"
	log "github.com/gophish/gophish/logger"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/models"
	templatefs "github.com/gophish/gophish/templates"
	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
)

// Session keys used while a user is part way through logging in with
// two-factor authentication, and while they're setting it up.
const (
	pendingTOTPUserKey     = "totp_pending_id"
	pendingTOTPExpiresKey  = "totp_pending_expires"
	pendingTOTPAttemptsKey = "totp_pending_attempts"
	pendingTOTPMethodKey   = "totp_pending_method"
	setupTOTPSecretKey     = "totp_setup_secret"
)

// secondFactorTimeout is how long a user has to enter their two-factor code
// after entering their password.
const secondFactorTimeout = 5 * time.Minute

// maxSecondFactorAttempts is the number of wrong two-factor codes a user can
// enter before they have to enter their password again.
const maxSecondFactorAttempts = 5

// clearSecondFactor removes any partially completed two-factor login from the
// session.
func clearSecondFactor(session *sessions.Session) {
	delete(session.Values, pendingTOTPUserKey)
	delete(session.Values, pendingTOTPExpiresKey)
	delete(session.Values, pendingTOTPAttemptsKey)
	delete(session.Values, pendingTOTPMethodKey)
}

// startSecondFactor records that the user has entered a correct password and
// sends them to enter their two-factor code. They aren't logged in until they
// do.
func (as *AdminServer) startSecondFactor(w http.ResponseWriter, r *http.Request, u models.User, next, method string) {
	session := ctx.Get(r, "session").(*sessions.Session)
	err := mid.RegenerateSession(session)
	if err != nil {
		log.Error(err)
		as.handleInvalidLogin(w, r, "Error creating session")
		return
	}
	delete(session.Values, "id")
	session.Values[pendingTOTPUserKey] = u.Id
	session.Values[pendingTOTPExpiresKey] = time.Now().Add(secondFactorTimeout).Unix()
	session.Values[pendingTOTPAttemptsKey] = 0
	session.Values[pendingTOTPMethodKey] = method
	session.Save(r, w)
	target := "/login/2fa"
	if next != "" {
		target += "?" + url.Values{"next": {safeNext(next)}}.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// renderSecondFactor renders the page asking for a two-factor code.
func (as *AdminServer) renderSecondFactor(w http.ResponseWriter, r *http.Request, status int) {
	session := ctx.Get(r, "session").(*sessions.Session)
	params := struct {
		User    models.User
		Title   string
		Flashes []interface{}
		Token   string
	}{Title: "Two-Factor Authentication", Token: csrf.Token(r)}
	params.Flashes = session.Flashes()
	session.Save(r, w)
	templates := template.New("template")
	_, err := templates.ParseFS(templatefs.FS, "login_2fa.html", "flashes.html")
	if err != nil {
		log.Error(err)
	}
	w.WriteHeader(status)
	template.Must(templates, err).ExecuteTemplate(w, "base", params)
}

// restartLogin abandons a partially completed two-factor login and sends the
// user back to enter their password.
func (as *AdminServer) restartLogin(w http.ResponseWriter, r *http.Request, message string) {
	session := ctx.Get(r, "session").(*sessions.Session)
	clearSecondFactor(session)
	if message != "" {
		Flash(w, r, "danger", message)
	}
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// LoginSecondFactor handles the second step of logging in for users with
// two-factor authentication enabled, after they've entered their password.
func (as *AdminServer) LoginSecondFactor(w http.ResponseWriter, r *http.Request) {
	session := ctx.Get(r, "session").(*sessions.Session)
	uid, ok := session.Values[pendingTOTPUserKey].(int64)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	expires, _ := session.Values[pendingTOTPExpiresKey].(int64)
	if time.Now().Unix() > expires {
		as.restartLogin(w, r, "Your login expired. Please sign in again.")
		return
	}
	switch r.Method {
	case "GET":
		as.renderSecondFactor(w, r, http.StatusOK)
	case "POST":
		u, err := models.GetUser(uid)
		if err != nil || u.AccountLocked || !u.TOTPEnabled {
			as.restartLogin(w, r, "Please sign in again.")
			return
		}
		err = models.VerifySecondFactor(&u, r.FormValue("code"))
		if err != nil {
			mid.Audit(r, &u, "", models.AuditLoginFailed, "", "invalid two-factor code")
			attempts, _ := session.Values[pendingTOTPAttemptsKey].(int)
			attempts++
			if attempts >= maxSecondFactorAttempts {
				as.restartLogin(w, r, "Too many invalid codes. Please sign in again.")
				return
			}
			session.Values[pendingTOTPAttemptsKey] = attempts
			Flash(w, r, "danger", auth.ErrInvalidTOTPCode.Error())
			as.renderSecondFactor(w, r, http.StatusUnauthorized)
			return
		}
		method, _ := session.Values[pendingTOTPMethodKey].(string)
		as.completeLogin(w, r, u, r.FormValue("next"), method+" and two-factor code")
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// twoFactorResponse is returned by the two-factor settings handlers.
type twoFactorResponse struct {
	Success       bool     `json:"success"`
	Message       string   `json:"message"`
	Secret        string   `json:"secret,omitempty"`
	URI           string   `json:"uri,omitempty"`
	QRCode        string   `json:"qr_code,omitempty"`
	RecoveryCodes []string `json:"recovery_codes,omitempty"`
}

func twoFactorError(w http.ResponseWriter, message string, status int) {
	api.JSONResponse(w, twoFactorResponse{Success: false, Message: message}, status)
}

// SetupTOTP starts setting up two-factor authentication for the current user.
// It generates a new secret, which is kept in the session until the user
// confirms it with a code from their authenticator app.
func (as *AdminServer) SetupTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	u := ctx.Get(r, "user").(models.User)
	session := ctx.Get(r, "session").(*sessions.Session)
	if u.TOTPEnabled {
		twoFactorError(w, "Two-factor authentication is already enabled", http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(r.FormValue("current_password"), u.Hash); err != nil {
		twoFactorError(w, auth.ErrInvalidPassword.Error(), http.StatusBadRequest)
		return
	}
	secret := auth.GenerateTOTPSecret()
	uri := auth.TOTPURI(u.Username, secret)
	png, err := auth.TOTPQRCode(uri)
	if err != nil {
		log.Error(err)
		twoFactorError(w, "Error generating QR code", http.StatusInternalServerError)
		return
	}
	session.Values[setupTOTPSecretKey] = secret
	session.Save(r, w)
	api.JSONResponse(w, twoFactorResponse{
		Success: true,
		Secret:  secret,
		URI:     uri,
		QRCode:  "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	}, http.StatusOK)
}

// EnableTOTP finishes setting up two-factor authentication once the user has
// entered a valid code for the secret generated by SetupTOTP. The response
// contains the user's recovery codes, which are never shown again.
func (as *AdminServer) EnableTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	u := ctx.Get(r, "user").(models.User)
	session := ctx.Get(r, "session").(*sessions.Session)
	secret, ok := session.Values[setupTOTPSecretKey].(string)
	if !ok || u.TOTPEnabled {
		twoFactorError(w, "Start setting up two-factor authentication first", http.StatusBadRequest)
		return
	}
	step, err := auth.ValidateTOTP(secret, r.FormValue("code"), time.Now(), 0)
	if err != nil {
		twoFactorError(w, auth.ErrInvalidTOTPCode.Error(), http.StatusBadRequest)
		return
	}
	codes, err := models.EnableTOTP(&u, secret, step)
	if err != nil {
		log.Error(err)
		twoFactorError(w, "Error enabling two-factor authentication", http.StatusInternalServerError)
		return
	}
	delete(session.Values, setupTOTPSecretKey)
	session.Save(r, w)
	mid.Audit(r, &u, "", models.AuditTOTPEnabled, "", "")
	// Sessions signed in with only a password no longer meet the bar
	if err = mid.RevokeOtherSessions(r, u.Id); err != nil {
		log.Error(err)
	}
	api.JSONResponse(w, twoFactorResponse{
		Success:       true,
		Message:       "Two-factor authentication enabled",
		RecoveryCodes: codes,
	}, http.StatusOK)
}

// DisableTOTP turns off two-factor authentication for the current user. It
// requires both their password and a current code or recovery code.
func (as *AdminServer) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	u := ctx.Get(r, "user").(models.User)
	if !u.TOTPEnabled {
		twoFactorError(w, "Two-factor authentication is not enabled", http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(r.FormValue("current_password"), u.Hash); err != nil {
		twoFactorError(w, auth.ErrInvalidPassword.Error(), http.StatusBadRequest)
		return
	}
	if err := models.VerifySecondFactor(&u, r.FormValue("code")); err != nil {
		twoFactorError(w, auth.ErrInvalidTOTPCode.Error(), http.StatusBadRequest)
		return
	}
	if err := models.DisableTOTP(&u); err != nil {
		log.Error(err)
		twoFactorError(w, "Error disabling two-factor authentication", http.StatusInternalServerError)
		return
	}
	mid.Audit(r, &u, "", models.AuditTOTPDisabled, "", "")
	api.JSONResponse(w, twoFactorResponse{Success: true, Message: "Two-factor authentication disabled"}, http.StatusOK)
}

// RegenerateRecoveryCodes replaces the current user's recovery codes. It
// requires their password and a current code, so that a stolen session alone
// can't be used to mint codes.
func (as *AdminServer) RegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	u := ctx.Get(r, "user").(models.User)
	if !u.TOTPEnabled {
		twoFactorError(w, "Two-factor authentication is not enabled", http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(r.FormValue("current_password"), u.Hash); err != nil {
		twoFactorError(w, auth.ErrInvalidPassword.Error(), http.StatusBadRequest)
		return
	}
	if err := models.VerifySecondFactor(&u, r.FormValue("code")); err != nil {
		twoFactorError(w, auth.ErrInvalidTOTPCode.Error(), http.StatusBadRequest)
		return
	}
	codes, err := models.RegenerateRecoveryCodes(u.Id)
	if err != nil {
		log.Error(err)
		twoFactorError(w, "Error generating recovery codes", http.StatusInternalServerError)
		return
	}
	mid.Audit(r, &u, "", models.AuditRecoveryCodes, "", "")
	api.JSONResponse(w, twoFactorResponse{
		Success:       true,
		Message:       "New recovery codes generated",
		RecoveryCodes: codes,
	}, http.StatusOK)
}
