package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// Tenants lists every tenant or creates one. Both take the ManageTenants
// permission (enforced by the route), so only provider admins reach here.
func (as *Server) Tenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenants, err := models.GetTenants()
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Error fetching tenants"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, tenants, http.StatusOK)
	case http.MethodPost:
		t := models.Tenant{}
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		if err := models.PostTenant(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, tenantErrorStatus(err))
			return
		}
		JSONResponse(w, t, http.StatusCreated)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// Tenant returns, renames, or deletes a tenant. All take the ManageTenants
// permission (enforced by the route).
func (as *Server) Tenant(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	existing, err := models.GetTenant(id)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Tenant not found"}, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		JSONResponse(w, existing, http.StatusOK)
	case http.MethodPut:
		t := models.Tenant{}
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		t.Id = id
		if err := models.PutTenant(&t); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, tenantErrorStatus(err))
			return
		}
		JSONResponse(w, t, http.StatusOK)
	case http.MethodDelete:
		if err := models.DeleteTenant(id); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, tenantErrorStatus(err))
			return
		}
		JSONResponse(w, models.Response{Success: true, Message: "Tenant deleted"}, http.StatusOK)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// tenantErrorStatus maps a tenant error to an HTTP status code.
func tenantErrorStatus(err error) int {
	switch {
	case errors.Is(err, models.ErrTenantName), errors.Is(err, models.ErrTenantNotEmpty),
		errors.Is(err, models.ErrLastTenant):
		return http.StatusBadRequest
	case errors.Is(err, models.ErrTenantNameTaken):
		return http.StatusConflict
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
