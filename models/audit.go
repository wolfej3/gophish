package models

import (
	"time"

	log "github.com/gophish/gophish/logger"
)

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

// DeleteOldAuditLogs deletes audit log entries older than retentionDays. A
// retentionDays of zero or less keeps entries forever and deletes nothing. It
// returns the number of rows removed.
func DeleteOldAuditLogs(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	result := db.Where("created_at < ?", cutoff).Delete(&AuditLog{})
	return result.RowsAffected, result.Error
}

// auditCleanupInterval is how often the background cleanup runs. It's a var so
// tests can lower it.
var auditCleanupInterval = 24 * time.Hour

// StartAuditLogCleanup prunes audit log entries older than retentionDays now and
// then once per day in the background. It's a no-op when retention isn't set, so
// the default behavior (keep everything) is unchanged.
func StartAuditLogCleanup(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	prune := func() {
		n, err := DeleteOldAuditLogs(retentionDays)
		if err != nil {
			log.Errorf("error pruning audit logs: %v", err)
			return
		}
		if n > 0 {
			log.Infof("pruned %d audit log entries older than %d days", n, retentionDays)
		}
	}
	prune()
	go func() {
		ticker := time.NewTicker(auditCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			prune()
		}
	}()
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
