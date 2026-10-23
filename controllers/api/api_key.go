package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// apiKeyRequest is the payload for creating a scoped API key.
type apiKeyRequest struct {
	Name      string   `json:"name"`
	Access    string   `json:"access"`
	Resources []string `json:"resources"`
	// ExpiresInDays sets when the key stops working. Zero means never.
	ExpiresInDays int `json:"expires_in_days"`
}

// apiKeyResponse is returned when a scoped API key is created. Key is the
// only time the key itself is returned.
type apiKeyResponse struct {
	Key    string        `json:"key"`
	APIKey models.APIKey `json:"api_key"`
}

// APIKeys lists the current user's scoped API keys, or creates a new one.
// Scoped keys can't be managed with a scoped key, so that a key can't be
// used to create a more powerful one.
func (as *Server) APIKeys(w http.ResponseWriter, r *http.Request) {
	uid := ctx.Get(r, "user_id").(int64)
	switch r.Method {
	case http.MethodGet:
		keys, err := models.GetAPIKeys(uid)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Error fetching API keys"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, keys, http.StatusOK)
	case http.MethodPost:
		req := apiKeyRequest{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid JSON structure"}, http.StatusBadRequest)
			return
		}
		if req.ExpiresInDays < 0 {
			JSONResponse(w, models.Response{Success: false, Message: "expires_in_days can't be negative"}, http.StatusBadRequest)
			return
		}
		k := models.APIKey{UserId: uid, Name: req.Name, Access: req.Access}
		if req.ExpiresInDays > 0 {
			expires := time.Now().UTC().AddDate(0, 0, req.ExpiresInDays)
			k.ExpiresAt = &expires
		}
		key, err := models.CreateAPIKey(&k, req.Resources)
		switch {
		case errors.Is(err, models.ErrAPIKeyName), errors.Is(err, models.ErrAPIKeyAccess), errors.Is(err, models.ErrAPIKeyResource):
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		case err != nil:
			JSONResponse(w, models.Response{Success: false, Message: "Error creating API key"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, apiKeyResponse{Key: key, APIKey: k}, http.StatusCreated)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// APIKey deletes one of the current user's scoped API keys.
func (as *Server) APIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	err := models.DeleteAPIKey(id, ctx.Get(r, "user_id").(int64))
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		JSONResponse(w, models.Response{Success: false, Message: "API key not found"}, http.StatusNotFound)
	case err != nil:
		JSONResponse(w, models.Response{Success: false, Message: "Error deleting API key"}, http.StatusInternalServerError)
	default:
		JSONResponse(w, models.Response{Success: true, Message: "API key deleted"}, http.StatusOK)
	}
}
