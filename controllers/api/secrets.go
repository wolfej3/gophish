package api

import "github.com/gophish/gophish/models"

// secretPlaceholder is returned in place of a stored secret, so API responses
// never expose SMTP passwords or Graph client secrets. When a client sends it
// back unchanged on an update, the existing stored value is kept.
const secretPlaceholder = "********"

// redactSMTP replaces a sending profile's secrets with the placeholder for
// responses.
func redactSMTP(s *models.SMTP) {
	if s.Password != "" {
		s.Password = secretPlaceholder
	}
	if s.ClientSecret != "" {
		s.ClientSecret = secretPlaceholder
	}
}

// redactSMTPs redacts a slice of sending profiles.
func redactSMTPs(ss []models.SMTP) {
	for i := range ss {
		redactSMTP(&ss[i])
	}
}
