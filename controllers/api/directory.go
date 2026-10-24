package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/directory"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// directoryConnectorInfo describes a connector to the admin interface,
// without its secrets.
type directoryConnectorInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// DirectoryConnectors lists the configured directory connectors by name, so
// the group editor can offer them. Secrets are never returned.
func (as *Server) DirectoryConnectors(w http.ResponseWriter, r *http.Request) {
	infos := []directoryConnectorInfo{}
	for _, c := range as.directory {
		infos = append(infos, directoryConnectorInfo{Name: c.Name, Provider: c.Provider})
	}
	JSONResponse(w, infos, http.StatusOK)
}

// connectorByName returns the configured connector with the given name.
func (as *Server) connectorByName(name string) (config.DirectoryConnector, bool) {
	for _, c := range as.directory {
		if c.Name == name {
			return c, true
		}
	}
	return config.DirectoryConnector{}, false
}

// GroupSource gets, sets, or clears a group's directory sync source.
func (as *Server) GroupSource(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	// Scoping the group to the team confirms the caller can reach it.
	if _, err := models.GetGroup(id, teamID(r)); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Group not found"}, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		source, err := models.GetDirectorySource(id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			JSONResponse(w, models.Response{Success: false, Message: "This group doesn't sync from a directory"}, http.StatusNotFound)
			return
		}
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Error fetching source"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, source, http.StatusOK)
	case http.MethodPut, http.MethodPost:
		source := models.DirectorySource{}
		if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		source.GroupId = id
		if _, ok := as.connectorByName(source.Connector); !ok {
			JSONResponse(w, models.Response{Success: false, Message: "Unknown directory connector"}, http.StatusBadRequest)
			return
		}
		if err := models.SetDirectorySource(&source); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		JSONResponse(w, source, http.StatusOK)
	case http.MethodDelete:
		if err := models.DeleteDirectorySource(id); err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Error removing source"}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, models.Response{Success: true, Message: "Directory source removed"}, http.StatusOK)
	default:
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
	}
}

// GroupSync syncs a group's members from its directory now.
func (as *Server) GroupSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 0, 64)
	group, err := models.GetGroup(id, teamID(r))
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Group not found"}, http.StatusNotFound)
		return
	}
	source, err := models.GetDirectorySource(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		JSONResponse(w, models.Response{Success: false, Message: "This group doesn't sync from a directory"}, http.StatusBadRequest)
		return
	}
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error fetching source"}, http.StatusInternalServerError)
		return
	}
	connector, ok := as.connectorByName(source.Connector)
	if !ok {
		JSONResponse(w, models.Response{Success: false, Message: "The connector \"" + source.Connector + "\" is no longer configured"}, http.StatusBadRequest)
		return
	}
	if err := directory.SyncGroup(connector, source, &group); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Sync failed: " + err.Error()}, http.StatusBadGateway)
		return
	}
	// Return the freshly synced group
	synced, err := models.GetGroup(id, teamID(r))
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error reloading group"}, http.StatusInternalServerError)
		return
	}
	JSONResponse(w, synced, http.StatusOK)
}
