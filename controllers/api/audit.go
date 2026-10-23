package api

import (
	"net/http"
	"strconv"

	"github.com/gophish/gophish/models"
)

// AuditLog returns entries from the audit log, newest first. The before_id,
// limit, username, and action query parameters page through and filter the
// entries.
func (as *Server) AuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONResponse(w, models.Response{Success: false, Message: http.StatusText(http.StatusMethodNotAllowed)}, http.StatusMethodNotAllowed)
		return
	}
	q := models.AuditLogQuery{
		Username: r.URL.Query().Get("username"),
		Action:   r.URL.Query().Get("action"),
	}
	q.BeforeID, _ = strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	q.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := models.GetAuditLogs(q)
	if err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Error fetching audit log"}, http.StatusInternalServerError)
		return
	}
	JSONResponse(w, logs, http.StatusOK)
}
