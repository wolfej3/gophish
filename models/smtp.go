package models

import (
	"crypto/tls"
	"errors"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gophish/gomail"
	"github.com/gophish/gophish/dialer"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/mailer"
	"github.com/gophish/gophish/sender"
	"gorm.io/gorm"
)

// Dialer is a wrapper around a standard gomail.Dialer in order
// to implement the mailer.Dialer interface. This allows us to better
// separate the mailer package as opposed to forcing a connection
// between mailer and gomail.
type Dialer struct {
	*gomail.Dialer
}

// Dial wraps the gomail dialer's Dial command
func (d *Dialer) Dial() (mailer.Sender, error) {
	return d.Dialer.Dial()
}

// SMTP contains the attributes needed to handle the sending of campaign emails
type SMTP struct {
	Id     int64 `json:"id" gorm:"column:id; primary_key:yes"`
	UserId int64 `json:"-" gorm:"column:user_id"`
	// TeamId is the team the object belongs to; UserId is who created it.
	TeamId           int64     `json:"team_id" gorm:"column:team_id"`
	Interface        string    `json:"interface_type" gorm:"column:interface_type"`
	Name             string    `json:"name"`
	Host             string    `json:"host"`
	Username         string    `json:"username,omitempty"`
	Password         string    `json:"password,omitempty"`
	FromAddress      string    `json:"from_address"`
	IgnoreCertErrors bool      `json:"ignore_cert_errors"`
	Headers          []Header  `json:"headers"`
	ModifiedDate     time.Time `json:"modified_date"`

	// The following are used by the API-based transports (Microsoft Graph
	// and Gmail) instead of Host/Username/Password. For those, FromAddress
	// is the mailbox to send as.
	TenantID        string `json:"tenant_id,omitempty" gorm:"column:tenant_id"`
	ClientID        string `json:"client_id,omitempty" gorm:"column:client_id"`
	ClientSecret    string `json:"client_secret,omitempty" gorm:"column:client_secret"`
	CredentialsFile string `json:"credentials_file,omitempty" gorm:"column:credentials_file"`

	// The following are used by the SMS transport. SMSProvider selects the
	// SMS API (e.g. "twilio"); SMSFrom is the sender phone number or ID. The
	// provider credentials reuse Username (e.g. Twilio Account SID) and
	// Password (e.g. Twilio Auth Token).
	SMSProvider string `json:"sms_provider,omitempty" gorm:"column:sms_provider"`
	SMSFrom     string `json:"sms_from,omitempty" gorm:"column:sms_from"`
}

// Sending profile interfaces (transports).
const (
	// SMTPInterface sends over SMTP (the default).
	SMTPInterface = "SMTP"
	// GraphInterface sends through the Microsoft Graph API.
	GraphInterface = "Microsoft Graph"
	// GmailInterface sends through the Gmail API.
	GmailInterface = "Gmail"
	// SMSInterface sends SMS text messages through an SMS provider API
	// instead of email.
	SMSInterface = "SMS"
)

// SMS providers supported by an SMS sending profile.
const (
	// SMSProviderTwilio sends through the Twilio REST API.
	SMSProviderTwilio = "twilio"
)

// Header contains the fields and methods for a sending profile to have
// custom headers
type Header struct {
	Id     int64  `json:"-"`
	SMTPId int64  `json:"-"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

// ErrFromAddressNotSpecified is thrown when there is no "From" address
// specified in the SMTP configuration
var ErrFromAddressNotSpecified = errors.New("No From Address specified")

// ErrInvalidFromAddress is thrown when the SMTP From field in the sending
// profiles containes a value that is not an email address
var ErrInvalidFromAddress = errors.New("Invalid SMTP From address because it is not an email address")

// ErrHostNotSpecified is thrown when there is no Host specified
// in the SMTP configuration
var ErrHostNotSpecified = errors.New("No SMTP Host specified")

// ErrInvalidHost indicates that the SMTP server string is invalid
var ErrInvalidHost = errors.New("Invalid SMTP server address")

// TableName specifies the database tablename for Gorm to use
func (s SMTP) TableName() string {
	return "smtp"
}

// ErrGraphCredentials is thrown when a Microsoft Graph profile is missing
// its tenant, client, or secret.
var ErrGraphCredentials = errors.New("Microsoft Graph profiles need a tenant ID, client ID, and client secret")

// ErrGmailCredentials is thrown when a Gmail profile is missing its service
// account credentials file.
var ErrGmailCredentials = errors.New("Gmail profiles need a service account credentials file")

// ErrSMSProviderNotSupported is thrown when an SMS profile names an unknown provider.
var ErrSMSProviderNotSupported = errors.New("Unsupported SMS provider")

// ErrSMSFromNotSpecified is thrown when an SMS profile has no sender number.
var ErrSMSFromNotSpecified = errors.New("SMS profiles need a sender number (From)")

// ErrSMSCredentials is thrown when an SMS profile is missing its API credentials.
var ErrSMSCredentials = errors.New("SMS profiles need an account SID (Username) and auth token (Password)")

// Validate ensures that SMTP configs/connections are valid
func (s *SMTP) Validate() error {
	// SMS profiles don't send email, so they're validated on their own terms
	// (provider, sender number, and API credentials) rather than an email
	// From address and host.
	if s.Interface == SMSInterface {
		return s.validateSMS()
	}
	if s.FromAddress == "" {
		return ErrFromAddressNotSpecified
	}
	if !validateFromAddress(s.FromAddress) {
		return ErrInvalidFromAddress
	}
	if _, err := mail.ParseAddress(s.FromAddress); err != nil {
		return err
	}
	switch s.Interface {
	case GraphInterface:
		if s.TenantID == "" || s.ClientID == "" || s.ClientSecret == "" {
			return ErrGraphCredentials
		}
		return nil
	case GmailInterface:
		if s.CredentialsFile == "" {
			return ErrGmailCredentials
		}
		return nil
	}
	return s.validateSMTP()
}

// validateSMS validates an SMS sending profile.
func (s *SMTP) validateSMS() error {
	if s.SMSProvider != SMSProviderTwilio {
		return ErrSMSProviderNotSupported
	}
	if s.SMSFrom == "" {
		return ErrSMSFromNotSpecified
	}
	if s.Username == "" || s.Password == "" {
		return ErrSMSCredentials
	}
	return nil
}

// validateSMTP validates the host for an SMTP sending profile.
func (s *SMTP) validateSMTP() error {
	if s.Host == "" {
		return ErrHostNotSpecified
	}
	// Make sure addr is in host:port format
	hp := strings.Split(s.Host, ":")
	if len(hp) > 2 {
		return ErrInvalidHost
	} else if len(hp) < 2 {
		hp = append(hp, "25")
	}
	if _, err := strconv.Atoi(hp[1]); err != nil {
		return ErrInvalidHost
	}
	return nil
}

// validateFromAddress validates
func validateFromAddress(email string) bool {
	r, _ := regexp.Compile("^([a-zA-Z0-9_\\-\\.]+)@([a-zA-Z0-9_\\-\\.]+)\\.([a-zA-Z]{2,18})$")
	return r.MatchString(email)
}

// GetDialer returns a dialer for the given sending profile, choosing the
// transport from the profile's interface type.
func (s *SMTP) GetDialer() (mailer.Dialer, error) {
	switch s.Interface {
	case SMSInterface:
		// SMS profiles are sent through the SMS worker path, not the mailer.
		return nil, errors.New("SMS profiles are not sent over the mail dialer")
	case GraphInterface:
		return sender.NewGraphDialer(sender.GraphConfig{
			TenantID:     s.TenantID,
			ClientID:     s.ClientID,
			ClientSecret: s.ClientSecret,
			From:         s.FromAddress,
		})
	case GmailInterface:
		return sender.NewGmailDialer(sender.GmailConfig{
			CredentialsFile: s.CredentialsFile,
			From:            s.FromAddress,
		})
	}
	// Setup the message and dial
	hp := strings.Split(s.Host, ":")
	if len(hp) < 2 {
		hp = append(hp, "25")
	}
	host := hp[0]
	// Any issues should have been caught in validation, but we'll
	// double check here.
	port, err := strconv.Atoi(hp[1])
	if err != nil {
		log.Error(err)
		return nil, err
	}
	dialer := dialer.Dialer()
	d := gomail.NewWithDialer(dialer, host, port, s.Username, s.Password)
	d.TLSConfig = &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: s.IgnoreCertErrors,
	}
	hostname, err := os.Hostname()
	if err != nil {
		log.Error(err)
		hostname = "localhost"
	}
	d.LocalName = hostname
	return &Dialer{d}, err
}

// GetSMTPs returns the SMTPs owned by the given team.
func GetSMTPs(tid int64) ([]SMTP, error) {
	ss := []SMTP{}
	err := db.Where("team_id=?", tid).Find(&ss).Error
	if err != nil {
		log.Error(err)
		return ss, err
	}
	for i := range ss {
		err = db.Where("smtp_id=?", ss[i].Id).Find(&ss[i].Headers).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			log.Error(err)
			return ss, err
		}
	}
	return ss, nil
}

// GetSMTP returns the SMTP, if it exists, specified by the given id and team_id.
func GetSMTP(id int64, tid int64) (SMTP, error) {
	s := SMTP{}
	err := db.Where("team_id=? and id=?", tid, id).Take(&s).Error
	if err != nil {
		log.Error(err)
		return s, err
	}
	err = db.Where("smtp_id=?", s.Id).Find(&s.Headers).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		log.Error(err)
		return s, err
	}
	return s, err
}

// GetSMTPByName returns the SMTP, if it exists, specified by the given name and team_id.
func GetSMTPByName(n string, tid int64) (SMTP, error) {
	s := SMTP{}
	err := db.Where("team_id=? and name=?", tid, n).Take(&s).Error
	if err != nil {
		log.Error(err)
		return s, err
	}
	err = db.Where("smtp_id=?", s.Id).Find(&s.Headers).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		log.Error(err)
	}
	return s, err
}

// PostSMTP creates a new SMTP in the database.
func PostSMTP(s *SMTP) error {
	err := s.Validate()
	if err != nil {
		log.Error(err)
		return err
	}
	// Insert into the DB
	err = db.Save(s).Error
	if err != nil {
		log.Error(err)
	}
	// Save custom headers
	for i := range s.Headers {
		s.Headers[i].SMTPId = s.Id
		err := db.Save(&s.Headers[i]).Error
		if err != nil {
			log.Error(err)
			return err
		}
	}
	return err
}

// PutSMTP edits an existing SMTP in the database.
// Per the PUT Method RFC, it presumes all data for a SMTP is provided.
func PutSMTP(s *SMTP) error {
	err := s.Validate()
	if err != nil {
		log.Error(err)
		return err
	}
	err = db.Where("id=?", s.Id).Save(s).Error
	if err != nil {
		log.Error(err)
	}
	// Delete all custom headers, and replace with new ones
	err = db.Where("smtp_id=?", s.Id).Delete(&Header{}).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		log.Error(err)
		return err
	}
	// Save custom headers
	for i := range s.Headers {
		s.Headers[i].SMTPId = s.Id
		err := db.Save(&s.Headers[i]).Error
		if err != nil {
			log.Error(err)
			return err
		}
	}
	return err
}

// DeleteSMTP deletes an existing SMTP in the database.
// An error is returned if a SMTP with the given user id and SMTP id is not found.
func DeleteSMTP(id int64, tid int64) error {
	// Delete all custom headers
	err := db.Where("smtp_id=?", id).Delete(&Header{}).Error
	if err != nil {
		log.Error(err)
		return err
	}
	err = db.Where("team_id=?", tid).Delete(&SMTP{Id: id}).Error
	if err != nil {
		log.Error(err)
	}
	return err
}
