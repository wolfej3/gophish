package api

import (
	"net/http"

	ctx "github.com/gophish/gophish/context"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/models"
)

// teamID returns the team the request acts in, as resolved by the
// RequireTeam middleware.
func teamID(r *http.Request) int64 {
	id, _ := ctx.Get(r, "team_id").(int64)
	return id
}

// userID returns the ID of the user making the request, used to record who
// created or last changed an object.
func userID(r *http.Request) int64 {
	id, _ := ctx.Get(r, "user_id").(int64)
	return id
}

// denyScopedKeyUserWrite rejects a request that tries to create or change a
// user account with a scoped (gpk_) API key. User management can change
// passwords, roles, two-factor, and SSO links, which would let a limited key
// take over the account, so it's restricted to full-key and session auth.
func denyScopedKeyUserWrite(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := ctx.Get(r, mid.ScopedAPIKeyContextKey).(models.APIKey); ok {
		JSONResponse(w, models.Response{Success: false, Message: "A limited API key can't manage user accounts"}, http.StatusForbidden)
		return true
	}
	return false
}
