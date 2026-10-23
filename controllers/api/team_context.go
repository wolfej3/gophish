package api

import (
	"net/http"

	ctx "github.com/gophish/gophish/context"
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
