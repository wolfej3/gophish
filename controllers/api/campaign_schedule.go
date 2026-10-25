package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/mux"
)

// CampaignSchedules handles requests for the /api/schedules/ endpoint.
// A GET lists the team's schedules; a POST creates a new one.
func (as *Server) CampaignSchedules(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET":
		ss, err := models.GetCampaignSchedules(teamID(r))
		if err != nil {
			log.Error(err)
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, ss, http.StatusOK)

	case r.Method == "POST":
		s := models.CampaignSchedule{}
		err := json.NewDecoder(r.Body).Decode(&s)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid JSON structure"}, http.StatusBadRequest)
			return
		}
		err = models.PostCampaignSchedule(&s, userID(r), teamID(r))
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		JSONResponse(w, s, http.StatusCreated)
	}
}

// CampaignSchedule handles requests for a single schedule by "id".
func (as *Server) CampaignSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.ParseInt(vars["id"], 0, 64)
	s, err := models.GetCampaignSchedule(id, teamID(r))
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Schedule not found"}, http.StatusNotFound)
		return
	}
	switch {
	case r.Method == "GET":
		JSONResponse(w, s, http.StatusOK)

	case r.Method == "DELETE":
		err = models.DeleteCampaignSchedule(id, teamID(r))
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		log.Infof("Deleted campaign schedule with id: %d", id)
		JSONResponse(w, models.Response{Success: true, Message: "Schedule deleted successfully!"}, http.StatusOK)

	case r.Method == "PUT":
		s = models.CampaignSchedule{}
		err = json.NewDecoder(r.Body).Decode(&s)
		if err != nil {
			log.Errorf("error decoding schedule: %v", err)
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		s.Id = id
		err = models.PutCampaignSchedule(&s, teamID(r))
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		JSONResponse(w, s, http.StatusOK)
	}
}
