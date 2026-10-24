package controllers

import (
	"net/http"

	"github.com/gophish/gophish/auth"
	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/controllers/api"
	log "github.com/gophish/gophish/logger"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/models"
)

// APIKeyReveal returns the current user's full API key after re-verifying
// their password (and two-factor code, if enabled). The key is no longer
// rendered into admin pages, so revealing it is a deliberate, audited action.
func (as *AdminServer) APIKeyReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	u := ctx.Get(r, "user").(models.User)
	if err := auth.ValidatePassword(r.FormValue("current_password"), u.Hash); err != nil {
		api.JSONResponse(w, models.Response{Success: false, Message: auth.ErrInvalidPassword.Error()}, http.StatusBadRequest)
		return
	}
	if u.TOTPEnabled {
		if err := models.VerifySecondFactor(&u, r.FormValue("code")); err != nil {
			api.JSONResponse(w, models.Response{Success: false, Message: auth.ErrInvalidTOTPCode.Error()}, http.StatusBadRequest)
			return
		}
	}
	mid.Audit(r, &u, "", models.AuditAPIKeyRevealed, "", "")
	log.Infof("API key revealed for user %q", u.Username)
	api.JSONResponse(w, map[string]string{"api_key": u.ApiKey}, http.StatusOK)
}
