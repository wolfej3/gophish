package directory

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
)

// Default attribute names and member filter, which match Active Directory.
const (
	defaultLDAPUserFilter = "(&(objectClass=user)(memberOf=%s))"
	defaultEmailAttr      = "mail"
	defaultFirstNameAttr  = "givenName"
	defaultLastNameAttr   = "sn"
)

// ldapProvider syncs members from an LDAP directory, including Active
// Directory. A source's external_id is substituted into the user filter,
// which by default is the distinguished name of the group whose members to
// find.
type ldapProvider struct {
	conn config.DirectoryConnector
}

func newLDAPProvider(conn config.DirectoryConnector) (Provider, error) {
	if conn.URL == "" {
		return nil, errors.New("ldap: url is required (e.g. ldaps://dc.example.com:636)")
	}
	if conn.BaseDN == "" {
		return nil, errors.New("ldap: base_dn is required")
	}
	return &ldapProvider{conn: conn}, nil
}

func (p *ldapProvider) Members(ctx context.Context, externalID string) ([]models.BaseRecipient, error) {
	if externalID == "" {
		return nil, errors.New("ldap: the external_id must be the group (the distinguished name by default)")
	}
	conn, err := p.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if p.conn.BindDN != "" {
		if err := conn.Bind(p.conn.BindDN, p.conn.BindPassword); err != nil {
			return nil, fmt.Errorf("ldap: binding as %s: %w", p.conn.BindDN, err)
		}
	}

	emailAttr := attrOr(p.conn.EmailAttribute, defaultEmailAttr)
	firstAttr := attrOr(p.conn.FirstNameAttribute, defaultFirstNameAttr)
	lastAttr := attrOr(p.conn.LastNameAttribute, defaultLastNameAttr)
	filterTmpl := p.conn.UserFilter
	if filterTmpl == "" {
		filterTmpl = defaultLDAPUserFilter
	}
	filter := fmt.Sprintf(filterTmpl, ldap.EscapeFilter(externalID))

	search := ldap.NewSearchRequest(
		p.conn.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter,
		[]string{emailAttr, firstAttr, lastAttr},
		nil,
	)
	result, err := conn.SearchWithPaging(search, 500)
	if err != nil {
		return nil, fmt.Errorf("ldap: searching: %w", err)
	}
	recipients := []models.BaseRecipient{}
	for _, entry := range result.Entries {
		email := entry.GetAttributeValue(emailAttr)
		if email == "" {
			continue
		}
		recipients = append(recipients, models.BaseRecipient{
			Email:     email,
			FirstName: entry.GetAttributeValue(firstAttr),
			LastName:  entry.GetAttributeValue(lastAttr),
		})
	}
	return recipients, nil
}

// dial connects to the LDAP server, applying TLS settings.
func (p *ldapProvider) dial() (*ldap.Conn, error) {
	tlsConfig := &tls.Config{InsecureSkipVerify: p.conn.InsecureTLS}
	conn, err := ldap.DialURL(p.conn.URL, ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, fmt.Errorf("ldap: connecting to %s: %w", p.conn.URL, err)
	}
	// StartTLS upgrades a plain ldap:// connection in place.
	if p.conn.StartTLS && strings.HasPrefix(strings.ToLower(p.conn.URL), "ldap://") {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ldap: starting TLS: %w", err)
		}
	}
	return conn, nil
}

func attrOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
