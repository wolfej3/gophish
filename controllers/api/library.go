package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gophish/gophish/library"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
)

// libraryConfigured responds with a consistent error when no library is set up,
// returning false if the caller should stop.
func (as *Server) libraryConfigured(w http.ResponseWriter) bool {
	if as.library == nil {
		JSONResponse(w, models.Response{Success: false, Message: "No template library is configured"}, http.StatusNotFound)
		return false
	}
	return true
}

// LibraryTemplates lists the templates available in the git-backed catalog.
func (as *Server) LibraryTemplates(w http.ResponseWriter, r *http.Request) {
	if !as.libraryConfigured(w) {
		return
	}
	if r.Method != http.MethodGet {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	templates, err := as.library.List()
	if err != nil {
		log.Errorf("error listing template library: %v", err)
		JSONResponse(w, models.Response{Success: false, Message: "Error reading template library: " + err.Error()}, http.StatusBadGateway)
		return
	}
	JSONResponse(w, templates, http.StatusOK)
}

// LibraryTemplate returns a single catalog template (with bodies) for preview,
// identified by the "name" query parameter.
func (as *Server) LibraryTemplate(w http.ResponseWriter, r *http.Request) {
	if !as.libraryConfigured(w) {
		return
	}
	if r.Method != http.MethodGet {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	t, err := as.library.Get(name)
	if err == library.ErrNotFound {
		JSONResponse(w, models.Response{Success: false, Message: "Template not found in library"}, http.StatusNotFound)
		return
	}
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error reading template: " + err.Error()}, http.StatusBadGateway)
		return
	}
	JSONResponse(w, t, http.StatusOK)
}

// LibraryRefresh pulls the latest catalog from the remote repository.
func (as *Server) LibraryRefresh(w http.ResponseWriter, r *http.Request) {
	if !as.libraryConfigured(w) {
		return
	}
	if r.Method != http.MethodPost {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	if err := as.library.Sync(); err != nil {
		log.Errorf("error refreshing template library: %v", err)
		JSONResponse(w, models.Response{Success: false, Message: "Error refreshing library: " + err.Error()}, http.StatusBadGateway)
		return
	}
	JSONResponse(w, models.Response{Success: true, Message: "Template library refreshed"}, http.StatusOK)
}

// libraryImportRequest is the body of an import request.
type libraryImportRequest struct {
	Name string `json:"name"`
}

// LibraryImport copies a catalog template into the caller's team as a new,
// independent Template. The copy is detached: later changes to the library
// don't affect it.
func (as *Server) LibraryImport(w http.ResponseWriter, r *http.Request) {
	if !as.libraryConfigured(w) {
		return
	}
	if r.Method != http.MethodPost {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	var req libraryImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Invalid JSON structure"}, http.StatusBadRequest)
		return
	}
	lt, err := as.library.Get(req.Name)
	if err == library.ErrNotFound {
		JSONResponse(w, models.Response{Success: false, Message: "Template not found in library"}, http.StatusNotFound)
		return
	}
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error reading template: " + err.Error()}, http.StatusBadGateway)
		return
	}

	tid := teamID(r)
	t := models.Template{
		Name:           uniqueTemplateName(lt.Name, tid),
		Subject:        lt.Subject,
		EnvelopeSender: lt.EnvelopeSender,
		Text:           lt.Text,
		HTML:           lt.HTML,
		UserId:         userID(r),
		TeamId:         tid,
	}
	for _, a := range lt.Attachments {
		t.Attachments = append(t.Attachments, models.Attachment{
			Name:    a.Name,
			Type:    a.Type,
			Content: base64.StdEncoding.EncodeToString(a.Content),
		})
	}
	// PostTemplate validates the name, bodies, template syntax, and attachment
	// sizes, so an oversized or malformed library entry is rejected here.
	if err := models.PostTemplate(&t); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
		return
	}
	log.Infof("Imported library template %q as %q into team %d", lt.Name, t.Name, tid)
	JSONResponse(w, t, http.StatusCreated)
}

// uniqueTemplateName returns name, or name with a numeric suffix if a template
// with that name already exists in the team, so imports never collide.
func uniqueTemplateName(name string, tid int64) string {
	if _, err := models.GetTemplateByName(name, tid); err != nil {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", name, i)
		if _, err := models.GetTemplateByName(candidate, tid); err != nil {
			return candidate
		}
	}
}
