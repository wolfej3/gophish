package api

import (
	"net/http"
	"net/url"

	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
)

// People returns everyone who has appeared in a campaign in the current
// team, with their cross-campaign activity.
func (as *Server) People(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	people, err := models.GetPeople(teamID(r))
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error fetching people"}, http.StatusInternalServerError)
		return
	}
	JSONResponse(w, people, http.StatusOK)
}

// Person returns one recipient's cross-campaign history and timeline. The
// email is taken from the path and may be URL-encoded.
func (as *Server) Person(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	email, err := url.PathUnescape(mux.Vars(r)["email"])
	if err != nil || email == "" {
		JSONResponse(w, models.Response{Success: false, Message: "Invalid email"}, http.StatusBadRequest)
		return
	}
	detail, err := models.GetPerson(teamID(r), email)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error fetching person"}, http.StatusInternalServerError)
		return
	}
	if len(detail.History) == 0 {
		JSONResponse(w, models.Response{Success: false, Message: "No activity for this person"}, http.StatusNotFound)
		return
	}
	JSONResponse(w, detail, http.StatusOK)
}
