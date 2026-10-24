package api

import (
	"encoding/json"
	"net/http"
	"time"

	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/imap"
	"github.com/gophish/gophish/models"
)

// IMAPServerValidate handles requests for the /api/imapserver/validate endpoint
func (as *Server) IMAPServerValidate(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET":
		JSONResponse(w, models.Response{Success: false, Message: "Only POSTs allowed"}, http.StatusBadRequest)
	case r.Method == "POST":
		im := models.IMAP{}
		err := json.NewDecoder(r.Body).Decode(&im)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid request"}, http.StatusBadRequest)
			return
		}
		err = imap.Validate(&im)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusOK)
			return
		}
		JSONResponse(w, models.Response{Success: true, Message: "Successful login."}, http.StatusCreated)
	}
}

// IMAPServer handles requests for the /api/imapserver/ endpoint
func (as *Server) IMAPServer(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET":
		ss, err := models.GetIMAP(ctx.Get(r, "user_id").(int64))
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		for i := range ss {
			if ss[i].Password != "" {
				ss[i].Password = secretPlaceholder
			}
		}
		JSONResponse(w, ss, http.StatusOK)

	// POST: Update database
	case r.Method == "POST":
		im := models.IMAP{}
		err := json.NewDecoder(r.Body).Decode(&im)
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: "Invalid data. Please check your IMAP settings."}, http.StatusBadRequest)
			return
		}
		// A placeholder password means "keep the stored value".
		if im.Password == secretPlaceholder {
			if existing, gerr := models.GetIMAP(ctx.Get(r, "user_id").(int64)); gerr == nil && len(existing) > 0 {
				im.Password = existing[0].Password
			}
		}
		im.ModifiedDate = time.Now().UTC()
		im.UserId = ctx.Get(r, "user_id").(int64)
		err = models.PostIMAP(&im, ctx.Get(r, "user_id").(int64))
		if err != nil {
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusInternalServerError)
			return
		}
		JSONResponse(w, models.Response{Success: true, Message: "Successfully saved IMAP settings."}, http.StatusCreated)
	}
}
