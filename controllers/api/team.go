package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// Teams lists the teams the current user can use, or creates a team.
// Listing is available to any user, so the team switcher can be populated.
// Creating takes the ModifySystem permission.
func (as *Server) Teams(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		u := ctx.Get(r, "user").(models.User)
		teams, err := models.GetUserTeams(u)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Error fetching teams"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, teams, http.StatusOK)
	case http.MethodPost:
		if !as.requireSystemAdmin(w, r) {
			return
		}
		t := models.Team{}
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		if err := models.PostTeam(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, teamErrorStatus(err))
			return
		}
		JSONResponse(w, t, http.StatusCreated)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// Team renames or deletes a team. Both take the ModifySystem permission.
func (as *Server) Team(w http.ResponseWriter, r *http.Request) {
	if !as.requireSystemAdmin(w, r) {
		return
	}
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	existing, err := models.GetTeam(id)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Team not found"}, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		JSONResponse(w, existing, http.StatusOK)
	case http.MethodPut:
		t := models.Team{}
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		t.Id = id
		if err := models.PutTeam(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, teamErrorStatus(err))
			return
		}
		JSONResponse(w, t, http.StatusOK)
	case http.MethodDelete:
		if err := models.DeleteTeam(id); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, teamErrorStatus(err))
			return
		}
		JSONResponse(w, models.Response{Success: true, Message: "Team deleted"}, http.StatusOK)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// teamMemberRequest adds or updates a member.
type teamMemberRequest struct {
	UserId int64  `json:"user_id"`
	Role   string `json:"role"`
}

// TeamMembers lists a team's members or adds one. Both take the ModifySystem
// permission.
func (as *Server) TeamMembers(w http.ResponseWriter, r *http.Request) {
	if !as.requireSystemAdmin(w, r) {
		return
	}
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	if _, err := models.GetTeam(id); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Team not found"}, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		members, err := models.GetTeamMembers(id)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Error fetching members"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, members, http.StatusOK)
	case http.MethodPost:
		req := teamMemberRequest{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		if _, err := models.GetUser(req.UserId); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "User not found"}, http.StatusNotFound)
			return
		}
		if err := models.SetTeamMember(id, req.UserId, req.Role); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, teamErrorStatus(err))
			return
		}
		JSONResponse(w, models.Response{Success: true, Message: "Member saved"}, http.StatusOK)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// TeamMember removes a member from a team. Takes the ModifySystem permission.
func (as *Server) TeamMember(w http.ResponseWriter, r *http.Request) {
	if !as.requireSystemAdmin(w, r) {
		return
	}
	if r.Method != http.MethodDelete {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	uid, _ := strconv.ParseInt(mux.Vars(r)["uid"], 0, 64)
	if err := models.RemoveTeamMember(id, uid); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error removing member"}, http.StatusInternalServerError)
		return
	}
	JSONResponse(w, models.Response{Success: true, Message: "Member removed"}, http.StatusOK)
}

// requireSystemAdmin writes a 403 and returns false if the current user
// can't modify the system.
func (as *Server) requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	u := ctx.Get(r, "user").(models.User)
	access, err := u.HasPermission(models.PermissionModifySystem)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
		return false
	}
	if !access {
		JSONResponse(w, models.Response{Success: false, Message: "You don't have permission to manage teams"}, http.StatusForbidden)
		return false
	}
	return true
}

// teamErrorStatus maps a team error to an HTTP status code.
func teamErrorStatus(err error) int {
	switch {
	case errors.Is(err, models.ErrTeamName), errors.Is(err, models.ErrTeamRole),
		errors.Is(err, models.ErrTeamNotEmpty), errors.Is(err, models.ErrLastTeam):
		return http.StatusBadRequest
	case errors.Is(err, models.ErrTeamNameTaken):
		return http.StatusConflict
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
