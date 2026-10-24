package models

import "time"

// Audit log actions. API requests that change something are recorded with
// AuditAPIRequest; the others record events in the admin interface.
const (
	AuditLogin            = "login"
	AuditLoginFailed      = "login_failed"
	AuditLogout           = "logout"
	AuditPasswordChanged  = "password_changed"
	AuditTOTPEnabled      = "2fa_enabled"
	AuditTOTPDisabled     = "2fa_disabled"
	AuditRecoveryCodes    = "recovery_codes_regenerated"
	AuditImpersonate      = "impersonate"
	AuditSSOUnlinked      = "sso_unlinked"
	AuditAPIKeyRevealed   = "api_key_revealed"
	AuditAPIKeyReset      = "api_key_reset"
	AuditAPIRequest       = "api_request"
	AuditAPIRequestDenied = "api_request_denied"
	AuditAPIRequestFailed = "api_request_failed"
)

// The default and maximum number of entries GetAuditLogs returns at once.
const (
	defaultAuditLogsLimit = 100
	maximumAuditLogsLimit = 1000
)

// AuditLog records something a user did, or tried to do, in Gophish.
type AuditLog struct {
	Id        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	// UserId is the account that did it, if known. Username is kept
	// separately so that entries survive the account being deleted, and so
	// that failed logins record the username that was tried.
	UserId    *int64 `json:"user_id"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Details   string `json:"details"`
	IPAddress string `json:"ip_address"`
}

// PutAuditLog records the audit log entry.
func PutAuditLog(a *AuditLog) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	return db.Create(a).Error
}

// AuditLogQuery filters the audit log entries returned by GetAuditLogs.
type AuditLogQuery struct {
	// BeforeID returns entries older than the one with this ID, for paging
	// back through the log. Zero starts from the newest entry.
	BeforeID int64
	// Limit is the maximum number of entries to return.
	Limit    int
	Username string
	Action   string
}

// GetAuditLogs returns audit log entries matching the query, newest first.
func GetAuditLogs(q AuditLogQuery) ([]AuditLog, error) {
	if q.Limit <= 0 {
		q.Limit = defaultAuditLogsLimit
	}
	if q.Limit > maximumAuditLogsLimit {
		q.Limit = maximumAuditLogsLimit
	}
	query := db.Order("id desc").Limit(q.Limit)
	if q.BeforeID > 0 {
		query = query.Where("id < ?", q.BeforeID)
	}
	if q.Username != "" {
		query = query.Where("username = ?", q.Username)
	}
	if q.Action != "" {
		query = query.Where("action = ?", q.Action)
	}
	logs := []AuditLog{}
	err := query.Find(&logs).Error
	return logs, err
}
