package models

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"

	"github.com/gophish/gophish/auth"
	"gorm.io/gorm"
)

// RecoveryCode is a single-use code that lets a user sign in when they don't
// have access to their two-factor authenticator. Like session tokens, only a
// hash of each code is stored.
type RecoveryCode struct {
	Id       int64
	UserId   int64
	CodeHash string
	UsedAt   *time.Time
}

// hashRecoveryCode returns the hex-encoded SHA-256 hash of a normalized
// recovery code. Recovery codes are random, so a fast hash is sufficient.
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(auth.NormalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

// replaceRecoveryCodes deletes the user's existing recovery codes and stores
// the provided ones in their place.
func replaceRecoveryCodes(tx *gorm.DB, uid int64, codes []string) error {
	if err := tx.Where("user_id = ?", uid).Delete(&RecoveryCode{}).Error; err != nil {
		return err
	}
	for _, c := range codes {
		rc := RecoveryCode{UserId: uid, CodeHash: hashRecoveryCode(c)}
		if err := tx.Create(&rc).Error; err != nil {
			return err
		}
	}
	return nil
}

// EnableTOTP turns on two-factor authentication for the user with the given
// secret, recording lastStep as the time step of the code used to confirm it
// so that the same code can't be used to sign in. It returns a new set of
// recovery codes, which should be shown to the user exactly once.
func EnableTOTP(u *User, secret string, lastStep int64) ([]string, error) {
	codes := auth.GenerateRecoveryCodes()
	err := db.Transaction(func(tx *gorm.DB) error {
		u.TOTPSecret = secret
		u.TOTPEnabled = true
		u.TOTPLastStep = lastStep
		if err := tx.Omit("Role").Save(u).Error; err != nil {
			return err
		}
		return replaceRecoveryCodes(tx, u.Id, codes)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP turns off two-factor authentication for the user and deletes
// their recovery codes.
func DisableTOTP(u *User) error {
	return db.Transaction(func(tx *gorm.DB) error {
		u.TOTPSecret = ""
		u.TOTPEnabled = false
		u.TOTPLastStep = 0
		if err := tx.Omit("Role").Save(u).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", u.Id).Delete(&RecoveryCode{}).Error
	})
}

// RegenerateRecoveryCodes replaces the user's recovery codes with a new set,
// invalidating the old ones.
func RegenerateRecoveryCodes(uid int64) ([]string, error) {
	codes := auth.GenerateRecoveryCodes()
	err := db.Transaction(func(tx *gorm.DB) error {
		return replaceRecoveryCodes(tx, uid, codes)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// CountRecoveryCodes returns the number of unused recovery codes the user has
// left.
func CountRecoveryCodes(uid int64) (int64, error) {
	var count int64
	err := db.Model(&RecoveryCode{}).Where("user_id = ? AND used_at IS NULL", uid).Count(&count).Error
	return count, err
}

// UseRecoveryCode marks the provided recovery code as used if it is one of
// the user's unused codes. It returns auth.ErrInvalidTOTPCode otherwise.
func UseRecoveryCode(uid int64, code string) error {
	hash := hashRecoveryCode(code)
	rcs := []RecoveryCode{}
	if err := db.Where("user_id = ? AND used_at IS NULL", uid).Find(&rcs).Error; err != nil {
		return err
	}
	for _, rc := range rcs {
		if subtle.ConstantTimeCompare([]byte(rc.CodeHash), []byte(hash)) != 1 {
			continue
		}
		// Only mark the code used if nobody else has in the meantime, so
		// that two simultaneous logins can't both spend the same code.
		now := time.Now().UTC()
		result := db.Model(&RecoveryCode{}).Where("id = ? AND used_at IS NULL", rc.Id).Update("used_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		break
	}
	return auth.ErrInvalidTOTPCode
}

// VerifySecondFactor checks a two-factor code or recovery code for the user.
// A TOTP code is accepted only once; its time step is saved so that it can't
// be replayed.
func VerifySecondFactor(u *User, code string) error {
	step, err := auth.ValidateTOTP(u.TOTPSecret, code, time.Now(), u.TOTPLastStep)
	if err == nil {
		// Advance the last used step only if no one else has used a code at
		// or after this step in the meantime.
		result := db.Model(&User{}).Where("id = ? AND totp_last_step < ?", u.Id, step).
			Update("totp_last_step", step)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return auth.ErrInvalidTOTPCode
		}
		u.TOTPLastStep = step
		return nil
	}
	return UseRecoveryCode(u.Id, code)
}
