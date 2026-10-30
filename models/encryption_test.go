package models

import (
	"github.com/gophish/gophish/encryption"
	"gopkg.in/check.v1"
)

// rawSMTPSecret reads a secret column of the smtp row straight from the
// database, bypassing the serializer, so a test can see the stored form.
func (s *ModelsSuite) rawSMTPSecret(c *check.C, id int64, column string) string {
	var v string
	err := db.Raw("SELECT "+column+" FROM smtp WHERE id = ?", id).Row().Scan(&v)
	c.Assert(err, check.Equals, nil)
	return v
}

func (s *ModelsSuite) TestSecretsEncryptedAtRest(c *check.C) {
	key, err := encryption.GenerateKey()
	c.Assert(err, check.Equals, nil)
	c.Assert(encryption.SetKey(key), check.Equals, nil)
	defer encryption.SetKey(nil)

	profile := SMTP{
		Name:         "Encrypted Profile",
		Interface:    SMTPInterface,
		Host:         "localhost:25",
		Username:     "user",
		Password:     "super-secret-pw",
		ClientSecret: "graph-client-secret",
		FromAddress:  "from@example.com",
		UserId:       1,
		TeamId:       1,
	}
	c.Assert(PostSMTP(&profile), check.Equals, nil)

	// The stored columns must be ciphertext, not the plaintext secrets.
	storedPw := s.rawSMTPSecret(c, profile.Id, "password")
	storedSecret := s.rawSMTPSecret(c, profile.Id, "client_secret")
	c.Assert(encryption.IsEncrypted(storedPw), check.Equals, true)
	c.Assert(encryption.IsEncrypted(storedSecret), check.Equals, true)
	c.Assert(storedPw == "super-secret-pw", check.Equals, false)

	// A non-secret column stays plaintext.
	c.Assert(s.rawSMTPSecret(c, profile.Id, "username"), check.Equals, "user")

	// Loading through GORM decrypts transparently.
	loaded := SMTP{}
	c.Assert(db.Table("smtp").Where("id = ?", profile.Id).Take(&loaded).Error, check.Equals, nil)
	c.Assert(loaded.Password, check.Equals, "super-secret-pw")
	c.Assert(loaded.ClientSecret, check.Equals, "graph-client-secret")
}

func (s *ModelsSuite) TestEncryptExistingSecretsUpgradesPlaintext(c *check.C) {
	key, err := encryption.GenerateKey()
	c.Assert(err, check.Equals, nil)

	// Store a profile while encryption is disabled, so the secret lands as
	// plaintext (as it would in a database written before this feature).
	encryption.SetKey(nil)
	profile := SMTP{
		Name:        "Legacy Profile",
		Interface:   SMTPInterface,
		Host:        "localhost:25",
		Password:    "legacy-pw",
		FromAddress: "from@example.com",
		UserId:      1,
		TeamId:      1,
	}
	c.Assert(PostSMTP(&profile), check.Equals, nil)
	c.Assert(encryption.IsEncrypted(s.rawSMTPSecret(c, profile.Id, "password")), check.Equals, false)

	// Enabling the key and running the startup pass encrypts it in place.
	c.Assert(encryption.SetKey(key), check.Equals, nil)
	defer encryption.SetKey(nil)
	c.Assert(encryptExistingSecrets(), check.Equals, nil)

	c.Assert(encryption.IsEncrypted(s.rawSMTPSecret(c, profile.Id, "password")), check.Equals, true)

	loaded := SMTP{}
	c.Assert(db.Table("smtp").Where("id = ?", profile.Id).Take(&loaded).Error, check.Equals, nil)
	c.Assert(loaded.Password, check.Equals, "legacy-pw")

	// Idempotent: a second pass leaves the already-encrypted value unchanged.
	before := s.rawSMTPSecret(c, profile.Id, "password")
	c.Assert(encryptExistingSecrets(), check.Equals, nil)
	c.Assert(s.rawSMTPSecret(c, profile.Id, "password"), check.Equals, before)
}
