package models

import (
	"fmt"

	"gorm.io/gorm"

	check "gopkg.in/check.v1"
)

func (s *ModelsSuite) TestPostSMTP(c *check.C) {
	smtp := SMTP{
		Name:        "Test SMTP",
		Host:        "1.1.1.1:25",
		FromAddress: "foo@example.com",
		UserId:      1,
		TeamId:      1,
	}
	err := PostSMTP(&smtp)
	c.Assert(err, check.Equals, nil)
	ss, err := GetSMTPs(1)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(ss), check.Equals, 1)
}

func (s *ModelsSuite) TestPostSMTPNoHost(c *check.C) {
	smtp := SMTP{
		Name:        "Test SMTP",
		FromAddress: "foo@example.com",
		UserId:      1,
		TeamId:      1,
	}
	err := PostSMTP(&smtp)
	c.Assert(err, check.Equals, ErrHostNotSpecified)
}

func (s *ModelsSuite) TestPostSMTPNoFrom(c *check.C) {
	smtp := SMTP{
		Name:   "Test SMTP",
		UserId: 1,
		TeamId: 1,
		Host:   "1.1.1.1:25",
	}
	err := PostSMTP(&smtp)
	c.Assert(err, check.Equals, ErrFromAddressNotSpecified)
}

func (s *ModelsSuite) TestPostInvalidFrom(c *check.C) {
	smtp := SMTP{
		Name:        "Test SMTP",
		Host:        "1.1.1.1:25",
		FromAddress: "Foo Bar <foo@example.com>",
		UserId:      1,
		TeamId:      1,
	}
	err := PostSMTP(&smtp)
	c.Assert(err, check.Equals, ErrInvalidFromAddress)
}

func (s *ModelsSuite) TestPostInvalidFromEmail(c *check.C) {
	smtp := SMTP{
		Name:        "Test SMTP",
		Host:        "1.1.1.1:25",
		FromAddress: "example.com",
		UserId:      1,
		TeamId:      1,
	}
	err := PostSMTP(&smtp)
	c.Assert(err, check.Equals, ErrInvalidFromAddress)
}

func (s *ModelsSuite) TestPostSMTPValidHeader(c *check.C) {
	smtp := SMTP{
		Name:        "Test SMTP",
		Host:        "1.1.1.1:25",
		FromAddress: "foo@example.com",
		UserId:      1,
		TeamId:      1,
		Headers: []Header{
			Header{Key: "Reply-To", Value: "test@example.com"},
			Header{Key: "X-Mailer", Value: "gophish"},
		},
	}
	err := PostSMTP(&smtp)
	c.Assert(err, check.Equals, nil)
	ss, err := GetSMTPs(1)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(ss), check.Equals, 1)
}

func (s *ModelsSuite) TestSMTPGetDialer(ch *check.C) {
	host := "localhost"
	port := 25
	smtp := SMTP{
		Host:             fmt.Sprintf("%s:%d", host, port),
		IgnoreCertErrors: false,
	}
	d, err := smtp.GetDialer()
	ch.Assert(err, check.Equals, nil)

	dialer := d.(*Dialer).Dialer
	ch.Assert(dialer.Host, check.Equals, host)
	ch.Assert(dialer.Port, check.Equals, port)
	ch.Assert(dialer.TLSConfig.ServerName, check.Equals, host)
	ch.Assert(dialer.TLSConfig.InsecureSkipVerify, check.Equals, smtp.IgnoreCertErrors)
}

func (s *ModelsSuite) TestGetInvalidSMTP(ch *check.C) {
	_, err := GetSMTP(-1, 1)
	ch.Assert(err, check.Equals, gorm.ErrRecordNotFound)
}

func (s *ModelsSuite) TestDefaultDeniedDial(ch *check.C) {
	host := "169.254.169.254"
	port := 25
	smtp := SMTP{
		Host: fmt.Sprintf("%s:%d", host, port),
	}
	d, err := smtp.GetDialer()
	ch.Assert(err, check.Equals, nil)
	_, err = d.Dial()
	ch.Assert(err, check.ErrorMatches, ".*upstream connection denied.*")
}

func (s *ModelsSuite) TestSMTPTransportValidation(ch *check.C) {
	// Graph needs tenant/client/secret
	graph := SMTP{Name: "g", TeamId: 1, UserId: 1, Interface: GraphInterface, FromAddress: "a@b.com"}
	ch.Assert(graph.Validate(), check.Equals, ErrGraphCredentials)
	graph.TenantID, graph.ClientID, graph.ClientSecret = "t", "c", "s"
	ch.Assert(graph.Validate(), check.Equals, nil)
	// Graph doesn't require a host
	ch.Assert(graph.Host, check.Equals, "")

	// Gmail needs a credentials file
	gmail := SMTP{Name: "m", TeamId: 1, UserId: 1, Interface: GmailInterface, FromAddress: "a@b.com"}
	ch.Assert(gmail.Validate(), check.Equals, ErrGmailCredentials)
	gmail.CredentialsFile = "/etc/gophish/sa.json"
	ch.Assert(gmail.Validate(), check.Equals, nil)

	// SMTP still requires a host
	smtp := SMTP{Name: "s", TeamId: 1, UserId: 1, Interface: SMTPInterface, FromAddress: "a@b.com"}
	ch.Assert(smtp.Validate(), check.Equals, ErrHostNotSpecified)
	smtp.Host = "smtp.example.com:587"
	ch.Assert(smtp.Validate(), check.Equals, nil)

	// An empty interface is treated as SMTP
	legacy := SMTP{Name: "l", TeamId: 1, UserId: 1, FromAddress: "a@b.com", Host: "smtp.example.com:25"}
	ch.Assert(legacy.Validate(), check.Equals, nil)
}

func (s *ModelsSuite) TestSMTPGetDialerByInterface(ch *check.C) {
	graph := SMTP{Interface: GraphInterface, FromAddress: "a@b.com", TenantID: "t", ClientID: "c", ClientSecret: "s"}
	d, err := graph.GetDialer()
	ch.Assert(err, check.Equals, nil)
	ch.Assert(d, check.NotNil)

	gmail := SMTP{Interface: GmailInterface, FromAddress: "a@b.com", CredentialsFile: "/nonexistent"}
	_, err = gmail.GetDialer()
	// The gmail dialer reads the credentials file at construction, so a
	// missing file is an error here.
	ch.Assert(err, check.NotNil)
}

func (s *ModelsSuite) TestSMTPTransportPersisted(ch *check.C) {
	graph := SMTP{Name: "Graph Profile", TeamId: 1, UserId: 1, Interface: GraphInterface,
		FromAddress: "a@b.com", TenantID: "tid", ClientID: "cid", ClientSecret: "secret"}
	ch.Assert(PostSMTP(&graph), check.Equals, nil)
	got, err := GetSMTP(graph.Id, 1)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(got.Interface, check.Equals, GraphInterface)
	ch.Assert(got.TenantID, check.Equals, "tid")
	ch.Assert(got.ClientID, check.Equals, "cid")
	ch.Assert(got.ClientSecret, check.Equals, "secret")
}
