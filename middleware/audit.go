package middleware

import (
	"net"
	"net/http"

	ctx "github.com/gophish/gophish/context"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/sessions"
)

// ImpersonatorKey is the session key holding the username of the admin who
// is impersonating the session's user.
const ImpersonatorKey = "impersonator"

// maxAuditTarget is the longest target we record, matching the column size.
const maxAuditTarget = 255

// Audit records an entry in the audit log for the request. u is the user who
// did it, if known; otherwise username records who they claimed to be.
// Failing to record an entry is logged but doesn't fail the request.
func Audit(r *http.Request, u *models.User, username, action, target, details string) {
	entry := &models.AuditLog{
		Username:  username,
		Action:    action,
		Target:    truncate(target, maxAuditTarget),
		Details:   details,
		IPAddress: remoteIP(r),
	}
	if u != nil {
		id := u.Id
		entry.UserId = &id
		entry.Username = u.Username
	}
	// Note when an admin is acting as another user
	if session, ok := ctx.Get(r, "session").(*sessions.Session); ok {
		if impersonator, ok := session.Values[ImpersonatorKey].(string); ok && u != nil {
			if session.Values["id"] == u.Id {
				if entry.Details != "" {
					entry.Details += "; "
				}
				entry.Details += "impersonated by " + impersonator
			}
		}
	}
	if err := models.PutAuditLog(entry); err != nil {
		log.Errorf("error recording audit log entry %q for %q: %v", action, entry.Username, err)
	}
}

// AuditUser records an entry in the audit log for the user making the
// request.
func AuditUser(r *http.Request, action, target, details string) {
	var u *models.User
	if user, ok := ctx.Get(r, "user").(models.User); ok {
		u = &user
	}
	Audit(r, u, "", action, target, details)
}

// statusRecorder remembers the status code written to a response.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// AuditAPIRequests records every API request that could change something in
// the audit log, along with whether it succeeded. It must come after
// RequireAPIKey so that the user is known.
func AuditAPIRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		action := models.AuditAPIRequest
		switch {
		case rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden:
			action = models.AuditAPIRequestDenied
		case rec.status >= 400:
			action = models.AuditAPIRequestFailed
		}
		details := http.StatusText(rec.status)
		if method, ok := ctx.Get(r, AuthMethodContextKey).(string); ok {
			details += "; via " + method
		}
		if k, ok := ctx.Get(r, ScopedAPIKeyContextKey).(models.APIKey); ok {
			details += " " + k.Prefix + " (" + k.Name + ")"
		}
		AuditUser(r, action, r.Method+" "+r.URL.Path, details)
	})
}

// remoteIP returns the IP address the request came from, without the port.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
