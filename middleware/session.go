package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"net/http"
	"time"

	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/models"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"gorm.io/gorm"
)

// sessionMaxAge is how long a session lasts without being saved, in seconds.
const sessionMaxAge = 86400 * 5

// init registers the necessary models to be saved in the session later
func init() {
	gob.Register(&models.User{})
	gob.Register(&models.Flash{})
}

// Store contains the session information for the request
var Store = NewDBStore()

// DBStore is a sessions.Store that keeps session data in the database. The
// cookie holds only a random token identifying the session, so sessions
// survive restarts and can be revoked by deleting them from the database.
type DBStore struct {
	Options *sessions.Options
}

// NewDBStore returns a DBStore with Gophish's default cookie options.
func NewDBStore() *DBStore {
	return &DBStore{
		Options: &sessions.Options{
			Path:     "/",
			MaxAge:   sessionMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		},
	}
}

// Get returns the named session for the request, loading it at most once per
// request.
func (s *DBStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New loads the session identified by the request's cookie. If there's no
// cookie, or it doesn't match an unexpired session, a new empty session is
// returned instead.
func (s *DBStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(s, name)
	opts := *s.Options
	session.Options = &opts
	session.IsNew = true
	c, err := r.Cookie(name)
	if err != nil || c.Value == "" {
		return session, nil
	}
	record, err := models.GetSessionByToken(c.Value)
	if err == gorm.ErrRecordNotFound {
		return session, nil
	}
	if err != nil {
		return session, err
	}
	values := make(map[interface{}]interface{})
	if err := gob.NewDecoder(bytes.NewReader(record.Data)).Decode(&values); err != nil {
		return session, err
	}
	session.Values = values
	session.ID = c.Value
	session.IsNew = false
	return session, nil
}

// Save stores the session and sets its cookie. Sessions with no values, or
// with a negative MaxAge, are deleted instead, so visitors who aren't logged
// in don't create stored sessions.
func (s *DBStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if session.Options.MaxAge < 0 || len(session.Values) == 0 {
		if session.ID != "" {
			if err := models.DeleteSessionByToken(session.ID); err != nil {
				return err
			}
			session.ID = ""
		}
		if _, err := r.Cookie(session.Name()); err == nil {
			opts := *session.Options
			opts.MaxAge = -1
			http.SetCookie(w, sessions.NewCookie(session.Name(), "", &opts))
		}
		return nil
	}
	if session.ID == "" {
		session.ID = newSessionToken()
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(session.Values); err != nil {
		return err
	}
	var uid *int64
	if id, ok := session.Values["id"].(int64); ok {
		uid = &id
	}
	maxAge := session.Options.MaxAge
	if maxAge == 0 {
		// A browser-session cookie still needs a server-side expiry
		maxAge = sessionMaxAge
	}
	expiresAt := time.Now().Add(time.Duration(maxAge) * time.Second)
	if err := models.PutSession(session.ID, uid, data.Bytes(), expiresAt); err != nil {
		return err
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), session.ID, session.Options))
	return nil
}

// newSessionToken returns a random 256-bit session token.
func newSessionToken() string {
	return base64.RawURLEncoding.EncodeToString(securecookie.GenerateRandomKey(32))
}

// RegenerateSession discards the session's current token so that the next
// save issues a new one. This is called when a user logs in, so that a token
// set before login (for example, by an attacker) can't be used afterward.
func RegenerateSession(session *sessions.Session) error {
	if session.ID != "" {
		if err := models.DeleteSessionByToken(session.ID); err != nil {
			return err
		}
	}
	session.ID = ""
	return nil
}

// RevokeOtherSessions signs the user out of every session except the one
// making this request, if it belongs to them. This is called after a password
// change so that anyone holding an old session loses access.
func RevokeOtherSessions(r *http.Request, uid int64) error {
	keep := ""
	if session, ok := ctx.Get(r, "session").(*sessions.Session); ok {
		if id, ok := session.Values["id"].(int64); ok && id == uid {
			keep = session.ID
		}
	}
	return models.DeleteUserSessions(uid, keep)
}
