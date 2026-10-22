package models

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
)

// Session is a server-side login session for the admin interface. The
// session's token lives only in the user's cookie; we store a hash of it so
// that the contents of the database can't be used to hijack sessions.
type Session struct {
	Id        int64
	TokenHash string
	UserId    *int64
	Data      []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// hashSessionToken returns the hex-encoded SHA-256 hash of a session token.
func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GetSessionByToken returns the unexpired session with the given token.
func GetSessionByToken(token string) (Session, error) {
	s := Session{}
	err := db.Where("token_hash = ? AND expires_at > ?", hashSessionToken(token), time.Now().UTC()).
		Take(&s).Error
	return s, err
}

// PutSession creates or updates the session with the given token. A nil uid
// indicates a session that isn't associated with a logged in user, such as
// one holding a flash message after logging out.
func PutSession(token string, uid *int64, data []byte, expiresAt time.Time) error {
	hash := hashSessionToken(token)
	s := Session{}
	err := db.Where("token_hash = ?", hash).Take(&s).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	s.TokenHash = hash
	s.UserId = uid
	s.Data = data
	s.ExpiresAt = expiresAt.UTC()
	if s.Id == 0 {
		s.CreatedAt = time.Now().UTC()
	}
	return db.Save(&s).Error
}

// DeleteSessionByToken deletes the session with the given token, if it
// exists.
func DeleteSessionByToken(token string) error {
	return db.Where("token_hash = ?", hashSessionToken(token)).Delete(&Session{}).Error
}

// DeleteUserSessions deletes every session belonging to the given user except
// the one with the token in keepToken, which may be empty to delete them all.
func DeleteUserSessions(uid int64, keepToken string) error {
	query := db.Where("user_id = ?", uid)
	if keepToken != "" {
		query = query.Where("token_hash <> ?", hashSessionToken(keepToken))
	}
	return query.Delete(&Session{}).Error
}

// DeleteExpiredSessions deletes sessions that have expired.
func DeleteExpiredSessions() error {
	return db.Where("expires_at <= ?", time.Now().UTC()).Delete(&Session{}).Error
}
