package models

import (
	"context"
	"path/filepath"
	"reflect"

	"github.com/gophish/gophish/encryption"
	log "github.com/gophish/gophish/logger"
	"gorm.io/gorm/schema"
)

// setupEncryption resolves the AES key for credential encryption at rest and
// upgrades any plaintext secrets already in the database. The default key
// file lives next to config.json; in test mode, or when the config path is
// unknown, no key is generated and encryption stays a no-op.
func setupEncryption() error {
	defaultKeyPath := ""
	if !conf.TestFlag && conf.ConfigPath != "" {
		defaultKeyPath = filepath.Join(filepath.Dir(conf.ConfigPath), "encryption.key")
	}
	source, err := encryption.Configure(conf.EncryptionKey, conf.EncryptionKeyFile, defaultKeyPath)
	if err != nil {
		return err
	}
	log.Infof("credential encryption at rest: %s", source)
	return encryptExistingSecrets()
}

// secretSerializer is a GORM field serializer that encrypts a string column
// at rest. Fields tagged `gorm:"serializer:secret"` are encrypted on write
// and decrypted on read transparently, so the model structs always hold
// plaintext and every load/save path (including association preloads) is
// covered without per-call handling. When no encryption key is configured the
// encryption package treats these as no-ops, so plaintext is stored and read
// unchanged.
type secretSerializer struct{}

// Scan decrypts a stored value into the destination string field.
func (secretSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) error {
	var stored string
	switch v := dbValue.(type) {
	case nil:
		stored = ""
	case []byte:
		stored = string(v)
	case string:
		stored = v
	default:
		stored = ""
	}
	plaintext, err := encryption.Decrypt(stored)
	if err != nil {
		return err
	}
	field.ReflectValueOf(ctx, dst).SetString(plaintext)
	return nil
}

// Value encrypts the plaintext field value for storage.
func (secretSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue interface{}) (interface{}, error) {
	plaintext, _ := fieldValue.(string)
	return encryption.Encrypt(plaintext)
}

func init() {
	// Registered unconditionally so the `serializer:secret` tag resolves
	// whenever these schemas are parsed, including in tests that never call
	// Setup. Whether anything is actually encrypted depends on whether a key
	// was configured.
	schema.RegisterSerializer("secret", secretSerializer{})
}

// encryptExistingSecrets upgrades any plaintext secret columns to the
// encrypted form in place. It runs at startup after migrations, only when an
// encryption key is configured, and is idempotent: already-encrypted and
// empty values are left untouched, so it is safe to run on every boot.
func encryptExistingSecrets() error {
	if !encryption.Enabled() {
		return nil
	}
	if err := encryptColumns("smtp", "id", "password", "client_secret"); err != nil {
		return err
	}
	return encryptColumns("imap", "user_id", "password")
}

// encryptColumns reads the named secret columns of a table with raw SQL
// (bypassing the serializer so it sees the stored form) and rewrites any
// plaintext value as ciphertext, matching rows by keyCol.
func encryptColumns(table, keyCol string, columns ...string) error {
	selectCols := keyCol
	for _, c := range columns {
		selectCols += ", " + c
	}
	rows, err := db.Raw("SELECT " + selectCols + " FROM " + table).Rows()
	if err != nil {
		log.Error(err)
		return err
	}
	defer rows.Close()

	type update struct {
		id     int64
		values []string
	}
	var updates []update
	for rows.Next() {
		scanDst := make([]interface{}, len(columns)+1)
		var id int64
		scanDst[0] = &id
		vals := make([]string, len(columns))
		for i := range columns {
			scanDst[i+1] = &vals[i]
		}
		if err := rows.Scan(scanDst...); err != nil {
			log.Error(err)
			return err
		}
		needsUpdate := false
		for i := range vals {
			if vals[i] != "" && !encryption.IsEncrypted(vals[i]) {
				enc, err := encryption.Encrypt(vals[i])
				if err != nil {
					log.Error(err)
					return err
				}
				vals[i] = enc
				needsUpdate = true
			}
		}
		if needsUpdate {
			updates = append(updates, update{id: id, values: append([]string(nil), vals...)})
		}
	}
	if err := rows.Err(); err != nil {
		log.Error(err)
		return err
	}

	for _, u := range updates {
		setClause := ""
		args := make([]interface{}, 0, len(columns)+1)
		for i, c := range columns {
			if i > 0 {
				setClause += ", "
			}
			setClause += c + "=?"
			args = append(args, u.values[i])
		}
		args = append(args, u.id)
		if err := db.Exec("UPDATE "+table+" SET "+setClause+" WHERE "+keyCol+"=?", args...).Error; err != nil {
			log.Error(err)
			return err
		}
	}
	if len(updates) > 0 {
		log.Infof("credential encryption: encrypted %d plaintext %s row(s) at rest", len(updates), table)
	}
	return nil
}
