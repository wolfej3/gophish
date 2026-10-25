package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gophish/gophish/auth"
	ctx "github.com/gophish/gophish/context"
	log "github.com/gophish/gophish/logger"
	mid "github.com/gophish/gophish/middleware"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// ErrUsernameTaken is thrown when a user attempts to register a username that is taken.
var ErrUsernameTaken = errors.New("Username already taken")

// ErrEmptyUsername is thrown when a user attempts to register a username that is taken.
var ErrEmptyUsername = errors.New("No username provided")

// ErrEmptyRole is throws when no role is provided when creating or modifying a user.
var ErrEmptyRole = errors.New("No role specified")

// ErrInsufficientPermission is thrown when a user attempts to change an
// attribute (such as the role) for which they don't have permission.
var ErrInsufficientPermission = errors.New("Permission denied")

// userRequest is the payload which represents the creation of a new user.
type userRequest struct {
	Username               string `json:"username"`
	Password               string `json:"password"`
	Role                   string `json:"role"`
	PasswordChangeRequired bool   `json:"password_change_required"`
	AccountLocked          bool   `json:"account_locked"`
	// DisableTOTP turns off two-factor authentication for the user, for when
	// they've lost their authenticator app and recovery codes.
	DisableTOTP bool `json:"disable_totp"`
	// UnlinkOIDC removes the links between the user and their single sign-on
	// identities (OIDC and SAML), so that the next identity to sign in with
	// their username is linked instead.
	UnlinkOIDC bool `json:"unlink_oidc"`
	// TenantId is the tenant to place the user in. Only a provider admin may
	// set it; for a tenant admin a new user always joins their own tenant.
	TenantId int64 `json:"tenant_id"`
	// DefaultTeamId is the team the user lands in by default. It must be a team
	// the user can access.
	DefaultTeamId int64 `json:"default_team_id"`
}

func (ur *userRequest) Validate(existingUser *models.User) error {
	switch {
	case ur.Username == "":
		return ErrEmptyUsername
	case ur.Role == "":
		return ErrEmptyRole
	}
	// Verify that the username isn't already taken. We consider two cases:
	// * We're creating a new user, in which case any match is a conflict
	// * We're modifying a user, in which case any match with a different ID is
	//   a conflict.
	possibleConflict, err := models.GetUserByUsername(ur.Username)
	if err == nil {
		if existingUser == nil {
			return ErrUsernameTaken
		}
		if possibleConflict.Id != existingUser.Id {
			return ErrUsernameTaken
		}
	}
	// If we have an error which is not simply indicating that no user was found, report it
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return nil
}

// Users contains functions to retrieve a list of existing users or create a
// new user. Users with the ModifySystem permissions can view and create users.
func (as *Server) Users(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET":
		// A provider admin sees every user; a tenant admin sees only their own
		// tenant's users.
		var us []models.User
		var err error
		if isProviderAdmin(r) {
			us, err = models.GetUsers()
		} else {
			currentUser := ctx.Get(r, "user").(models.User)
			us, err = models.GetUsersByTenant(currentUser.TenantId)
		}
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, us, http.StatusOK)
		return
	case r.Method == "POST":
		if denyScopedKeyUserWrite(w, r) {
			return
		}
		ur := &userRequest{}
		err := json.NewDecoder(r.Body).Decode(ur)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		err = ur.Validate(nil)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		err = auth.CheckPasswordPolicy(ur.Password)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		hash, err := auth.GeneratePasswordHash(ur.Password)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		role, err := models.GetRoleBySlug(ur.Role)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		// Only a provider admin may create another provider admin.
		if role.Slug == models.RoleProviderAdmin && !isProviderAdmin(r) {
			JSONResponse(w, models.Response{Success: false, Message: ErrInsufficientPermission.Error()}, http.StatusForbidden)
			return
		}
		// A tenant admin's new users join their own tenant; a provider admin may
		// place a user in any tenant.
		currentUser := ctx.Get(r, "user").(models.User)
		tenantID := currentUser.TenantId
		if isProviderAdmin(r) && ur.TenantId != 0 {
			tenantID = ur.TenantId
		}
		if _, err := models.GetTenant(tenantID); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid tenant"}, http.StatusBadRequest)
			return
		}
		user := models.User{
			Username:               ur.Username,
			Hash:                   hash,
			ApiKey:                 auth.GenerateSecureKey(auth.APIKeyLength),
			Role:                   role,
			RoleID:                 role.ID,
			TenantId:               tenantID,
			PasswordChangeRequired: ur.PasswordChangeRequired,
			AccountLocked:          ur.AccountLocked,
		}
		err = models.PutUser(&user)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		// Give the new user a private personal team. Their default team is left
		// unset until they (or an admin) choose one.
		if _, err := models.EnsurePersonalTeam(user); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, user, http.StatusOK)
		return
	}
}

// User contains functions to retrieve or delete a single user. Users with
// the ModifySystem permission can view and modify any user. Otherwise, users
// may only view or delete their own account.
func (as *Server) User(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.ParseInt(vars["id"], 0, 64)
	// If the user doesn't have ModifySystem permissions, we need to verify
	// that they're only taking action on their account.
	currentUser := ctx.Get(r, "user").(models.User)
	hasSystem, err := currentUser.HasPermission(models.PermissionModifySystem)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
		return
	}
	if !hasSystem && currentUser.Id != id {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusForbidden)}, http.StatusForbidden)
		return
	}
	existingUser, err := models.GetUser(id)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "User not found"}, http.StatusNotFound)
		return
	}
	// A tenant admin can only act on users within their own tenant. (A user
	// acting on their own account is always allowed.)
	if hasSystem && !isProviderAdmin(r) && existingUser.TenantId != currentUser.TenantId {
		JSONResponse(w, models.Response{Success: false, Message: "User not found"}, http.StatusNotFound)
		return
	}
	// A provider admin outranks a tenant admin. Only a provider admin may
	// modify or delete a provider admin's account; otherwise a tenant admin
	// sharing the bootstrap provider admin's tenant could delete or alter it.
	// (A tenant admin's own role is never provider_admin, so this never blocks
	// a user from managing their own account.)
	if r.Method != http.MethodGet && existingUser.Role.Slug == models.RoleProviderAdmin && !isProviderAdmin(r) {
		JSONResponse(w, models.Response{Success: false, Message: ErrInsufficientPermission.Error()}, http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && denyScopedKeyUserWrite(w, r) {
		return
	}
	switch {
	case r.Method == "GET":
		JSONResponse(w, existingUser, http.StatusOK)
	case r.Method == "DELETE":
		err = models.DeleteUser(id)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		log.Infof("Deleted user account for %s", existingUser.Username)
		JSONResponse(w, models.Response{Success: true, Message: "User deleted Successfully!"}, http.StatusOK)
	case r.Method == "PUT":
		ur := &userRequest{}
		err = json.NewDecoder(r.Body).Decode(ur)
		if err != nil {
			log.Errorf("error decoding user request: %v", err)
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		err = ur.Validate(&existingUser)
		if err != nil {
			log.Errorf("invalid user request received: %v", err)
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		existingUser.Username = ur.Username
		// Only users with the ModifySystem permission are able to update a
		// user's role. This prevents a privilege escalation letting users
		// upgrade their own account.
		if !hasSystem && ur.Role != existingUser.Role.Slug {
			JSONResponse(w, models.Response{Success: false, Message: ErrInsufficientPermission.Error()}, http.StatusBadRequest)
			return
		}
		role, err := models.GetRoleBySlug(ur.Role)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		// Only a provider admin can grant or revoke the provider_admin role.
		if (role.Slug == models.RoleProviderAdmin || existingUser.Role.Slug == models.RoleProviderAdmin) && !isProviderAdmin(r) {
			JSONResponse(w, models.Response{Success: false, Message: ErrInsufficientPermission.Error()}, http.StatusForbidden)
			return
		}
		// If our user is trying to change the role of an admin, we need to
		// ensure that it isn't the last user account with that privileged role.
		if existingUser.Role.Slug == models.RoleProviderAdmin && existingUser.Role.ID != role.ID {
			err = models.EnsureEnoughProviderAdmins()
			if err != nil {
				JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
				return
			}
		}
		if existingUser.Role.Slug == models.RoleAdmin && existingUser.Role.ID != role.ID {
			err = models.EnsureEnoughAdmins()
			if err != nil {
				JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
				return
			}
		}
		existingUser.Role = role
		existingUser.RoleID = role.ID
		// A provider admin may move the user to another tenant. This carries the
		// user's personal team across and drops their old-tenant team
		// memberships, so it also resets their default team.
		tenantMoved := false
		if isProviderAdmin(r) && ur.TenantId != 0 && ur.TenantId != existingUser.TenantId {
			if _, err := models.GetTenant(ur.TenantId); err != nil {
				JSONResponse(w, models.Response{Success: false, Message: "Invalid tenant"}, http.StatusBadRequest)
				return
			}
			if err := models.MoveUserToTenant(existingUser.Id, ur.TenantId); err != nil {
				JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
				return
			}
			existingUser.TenantId = ur.TenantId
			existingUser.DefaultTeamId = 0
			tenantMoved = true
		}
		// We don't force the password to be provided, since it may be an admin
		// managing the user's account, and making a simple change like
		// updating the username or role. However, if it _is_ provided, we'll
		// update the stored hash after validating the new password meets our
		// password policy.
		//
		// Note that we don't force the current password to be provided. The
		// assumption here is that the API key is a proper bearer token proving
		// authenticated access to the account.
		existingUser.PasswordChangeRequired = ur.PasswordChangeRequired
		if ur.Password != "" {
			err = auth.CheckPasswordPolicy(ur.Password)
			if err != nil {
				JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
				return
			}
			hash, err := auth.GeneratePasswordHash(ur.Password)
			if err != nil {
				JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
				return
			}
			existingUser.Hash = hash
		}
		existingUser.AccountLocked = ur.AccountLocked
		if ur.DisableTOTP && existingUser.TOTPEnabled {
			err = models.DisableTOTP(&existingUser)
			if err != nil {
				JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
				return
			}
			mid.AuditUser(r, models.AuditTOTPDisabled, existingUser.Username, "turned off by an admin")
		}
		if ur.UnlinkOIDC && (existingUser.OIDCIdentity != "" || existingUser.SAMLIdentity != "") {
			existingUser.OIDCIdentity = ""
			existingUser.SAMLIdentity = ""
			mid.AuditUser(r, models.AuditSSOUnlinked, existingUser.Username, "unlinked by an admin")
		}
		// A default team can be set, but only to a team the user can access.
		// Skip this when the user was just moved to another tenant: the move
		// reset their default team, and a requested one would belong to the old
		// tenant.
		if !tenantMoved && ur.DefaultTeamId != 0 && ur.DefaultTeamId != existingUser.DefaultTeamId {
			teams, terr := models.GetUserTeams(existingUser)
			if terr != nil {
				JSONResponse(w, models.Response{Success: false, Message: terr.Error()}, http.StatusInternalServerError)
				return
			}
			allowed := false
			for _, t := range teams {
				if t.Id == ur.DefaultTeamId {
					allowed = true
					break
				}
			}
			if !allowed {
				JSONResponse(w, models.Response{Success: false, Message: "Default team must be a team the user can access"}, http.StatusBadRequest)
				return
			}
			existingUser.DefaultTeamId = ur.DefaultTeamId
		}
		err = models.PutUser(&existingUser)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		// A locked account is signed out everywhere. A new password signs the
		// user out of every session other than this one.
		switch {
		case existingUser.AccountLocked:
			err = models.DeleteUserSessions(existingUser.Id, "")
		case ur.Password != "":
			err = mid.RevokeOtherSessions(r, existingUser.Id)
		}
		if err != nil {
			log.Errorf("error revoking sessions for user %d: %v", existingUser.Id, err)
		}
		JSONResponse(w, existingUser, http.StatusOK)
	}
}
