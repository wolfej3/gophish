package middleware

import (
	"net/http"
	"strconv"
	"strings"

	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/models"
)

// Context keys holding the team a request acts in, the user's role on it, and
// the tenant the request is scoped to.
const (
	TeamIDContextKey   = "team_id"
	TeamRoleContextKey = "team_role"
	TenantIDContextKey = "tenant_id"
)

// TeamIDHeader lets API clients choose which team a request acts in. The web
// interface sends it from the team switcher. Without it, the request uses the
// user's default team, then their first team.
const TeamIDHeader = "X-Gophish-Team-Id"

// TenantIDHeader lets a provider admin scope a request to one tenant (for
// filtering the team and user lists). It's ignored for everyone else, whose
// tenant is fixed to their own.
const TenantIDHeader = "X-Gophish-Tenant-Id"

// RequireTeam resolves which team the request acts in and stores it in the
// context, then enforces team membership and role for requests to team
// resources. It must come after RequireAPIKey.
func RequireTeam(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := ctx.Get(r, "user").(models.User)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		// Scope the request to a tenant. A provider admin may select one with
		// the tenant header; everyone else is fixed to their own tenant.
		tenantID := u.TenantId
		if provider, _ := u.HasPermission(models.PermissionManageTenants); provider {
			if rt := requestedTenantID(r); rt != 0 {
				tenantID = rt
			}
		}
		r = ctx.Set(r, TenantIDContextKey, tenantID)

		teams, err := models.GetUserTeams(u)
		if err != nil {
			JSONError(w, http.StatusInternalServerError, "Error loading teams")
			return
		}
		// Pick the requested team if the user can use it, then the user's
		// default team, then their first one. A requested team the user can't
		// use is ignored rather than rejected: the team id is sent
		// automatically from the browser and can go stale (e.g. a different user
		// signed in on the same browser), so it falls back to the user's own
		// team instead of 403-ing every request. Falling back only ever selects
		// a team the user already has, so it grants no extra access.
		var current *models.TeamMembership
		requested := requestedTeamID(r)
		for i := range teams {
			if requested != 0 && teams[i].Id == requested {
				current = &teams[i]
				break
			}
		}
		if current == nil {
			if u.DefaultTeamId != 0 {
				for i := range teams {
					if teams[i].Id == u.DefaultTeamId {
						current = &teams[i]
						break
					}
				}
			}
			if current == nil && len(teams) > 0 {
				current = &teams[0]
			}
		}

		resource := apiResource(r)
		isTeamResource := models.TeamObjectResources[resource]
		if current == nil {
			// Only a user who belongs to no team at all reaches here.
			if isTeamResource {
				JSONError(w, http.StatusForbidden, "You aren't a member of any team")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		r = ctx.Set(r, TeamIDContextKey, current.Id)
		r = ctx.Set(r, TeamRoleContextKey, current.Role)
		// Changing a team's objects takes the editor role
		if isTeamResource && isWrite(r) && current.Role != models.TeamRoleEditor {
			JSONError(w, http.StatusForbidden, "You have view-only access to this team")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestedTeamID returns the team ID the request asked for, from the header
// or a team_id form value, or 0 if none.
func requestedTeamID(r *http.Request) int64 {
	raw := r.Header.Get(TeamIDHeader)
	if raw == "" {
		raw = r.Form.Get("team_id")
	}
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

// requestedTenantID returns the tenant ID the request asked for, from the
// header or a tenant_id form value, or 0 if none.
func requestedTenantID(r *http.Request) int64 {
	raw := r.Header.Get(TenantIDHeader)
	if raw == "" {
		raw = r.Form.Get("tenant_id")
	}
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

// apiResource returns the first path segment after /api/, which names the
// resource being acted on.
func apiResource(r *http.Request) string {
	return strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/"), "/", 2)[0]
}

// isWrite returns whether the request changes something.
func isWrite(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// TeamID returns the team the request acts in, or 0 if none was resolved.
func TeamID(r *http.Request) int64 {
	id, _ := ctx.Get(r, TeamIDContextKey).(int64)
	return id
}

// TenantID returns the tenant the request is scoped to, or 0 if none was
// resolved.
func TenantID(r *http.Request) int64 {
	id, _ := ctx.Get(r, TenantIDContextKey).(int64)
	return id
}
