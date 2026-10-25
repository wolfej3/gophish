package models

import (
	"errors"
	"time"
)

// ErrModifyingOnlyAdmin occurs when there is an attempt to modify the only
// user account with the Admin role in such a way that there will be no user
// accounts left in Gophish with that role.
var ErrModifyingOnlyAdmin = errors.New("Cannot remove the only administrator")

// User represents the user model for gophish.
type User struct {
	Id       int64  `json:"id"`
	Username string `json:"username" gorm:"not null;unique"`
	Hash     string `json:"-"`
	ApiKey   string `json:"api_key" gorm:"not null;unique"`
	Role     Role   `json:"role"`
	RoleID   int64  `json:"-"`
	// TenantId is the tenant the user belongs to. Every user belongs to exactly
	// one tenant; provider admins act across all of them.
	TenantId int64 `json:"tenant_id" gorm:"column:tenant_id"`
	// DefaultTeamId is the team the user lands in when they don't ask for a
	// specific one. 0 falls back to their first available team.
	DefaultTeamId          int64     `json:"default_team_id" gorm:"column:default_team_id"`
	PasswordChangeRequired bool      `json:"password_change_required"`
	AccountLocked          bool      `json:"account_locked"`
	LastLogin              time.Time `json:"last_login"`
	TOTPSecret             string    `json:"-" gorm:"column:totp_secret"`
	TOTPEnabled            bool      `json:"totp_enabled" gorm:"column:totp_enabled"`
	TOTPLastStep           int64     `json:"-" gorm:"column:totp_last_step"`
	// OIDCIdentity is the issuer and subject of the single sign-on identity
	// linked to this account, if any.
	OIDCIdentity string `json:"oidc_identity" gorm:"column:oidc_identity"`
	// SAMLIdentity is the identity provider and NameID of the SAML identity
	// linked to this account, if any.
	SAMLIdentity string `json:"saml_identity" gorm:"column:saml_identity"`
}

// Single sign-on providers an account can be linked to.
const (
	SSOProviderOIDC = "oidc"
	SSOProviderSAML = "saml"
)

// SSOIdentity returns the identity from the single sign-on provider linked
// to the account, if any.
func (u *User) SSOIdentity(provider string) string {
	if provider == SSOProviderSAML {
		return u.SAMLIdentity
	}
	return u.OIDCIdentity
}

// SetSSOIdentity links the account to an identity from the single sign-on
// provider.
func (u *User) SetSSOIdentity(provider, identity string) {
	if provider == SSOProviderSAML {
		u.SAMLIdentity = identity
	} else {
		u.OIDCIdentity = identity
	}
}

// GetUser returns the user that the given id corresponds to. If no user is found, an
// error is thrown.
func GetUser(id int64) (User, error) {
	u := User{}
	err := db.Preload("Role").Where("id=?", id).First(&u).Error
	return u, err
}

// GetUsers returns the users registered in Gophish
func GetUsers() ([]User, error) {
	us := []User{}
	err := db.Preload("Role").Find(&us).Error
	return us, err
}

// GetUsersByTenant returns the users that belong to the given tenant. Tenant
// admins see only their own tenant's users; provider admins use GetUsers.
func GetUsersByTenant(tid int64) ([]User, error) {
	us := []User{}
	err := db.Preload("Role").Where("tenant_id = ?", tid).Find(&us).Error
	return us, err
}

// GetUserByAPIKey returns the user that the given API Key corresponds to. If no user is found, an
// error is thrown.
func GetUserByAPIKey(key string) (User, error) {
	u := User{}
	err := db.Preload("Role").Where("api_key = ?", key).First(&u).Error
	return u, err
}

// GetUserBySSOIdentity returns the user linked to the given identity from
// the single sign-on provider. If no user is found, an error is thrown.
func GetUserBySSOIdentity(provider, identity string) (User, error) {
	column := "oidc_identity"
	if provider == SSOProviderSAML {
		column = "saml_identity"
	}
	u := User{}
	err := db.Preload("Role").Where(column+" = ?", identity).First(&u).Error
	return u, err
}

// GetUserByUsername returns the user that the given username corresponds to. If no user is found, an
// error is thrown.
func GetUserByUsername(username string) (User, error) {
	u := User{}
	err := db.Preload("Role").Where("username = ?", username).First(&u).Error
	return u, err
}

// PutUser updates the given user. The user's role is referenced by RoleID and
// is never created or modified through the user.
func PutUser(u *User) error {
	err := db.Omit("Role").Save(u).Error
	return err
}

// EnsureEnoughAdmins ensures that there is more than one user account in
// Gophish with the Admin role. This function is meant to be called before
// modifying a user account with the Admin role in a non-revokable way.
func EnsureEnoughAdmins() error {
	role, err := GetRoleBySlug(RoleAdmin)
	if err != nil {
		return err
	}
	var adminCount int64
	err = db.Model(&User{}).Where("role_id=?", role.ID).Count(&adminCount).Error
	if err != nil {
		return err
	}
	if adminCount == 1 {
		return ErrModifyingOnlyAdmin
	}
	return nil
}

// ErrModifyingOnlyProviderAdmin occurs when there is an attempt to remove or
// demote the only provider admin, which would leave no one able to manage
// tenants.
var ErrModifyingOnlyProviderAdmin = errors.New("Cannot remove the only provider administrator")

// EnsureEnoughProviderAdmins ensures that more than one user account has the
// provider admin role. It's meant to be called before deleting or demoting a
// provider admin.
func EnsureEnoughProviderAdmins() error {
	role, err := GetRoleBySlug(RoleProviderAdmin)
	if err != nil {
		return err
	}
	var count int64
	err = db.Model(&User{}).Where("role_id=?", role.ID).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 1 {
		return ErrModifyingOnlyProviderAdmin
	}
	return nil
}

// DeleteUser deletes the given user. To ensure that there is always at least
// one user account with the Admin role, this function will refuse to delete
// the last Admin. Objects the user created stay with their teams.
func DeleteUser(id int64) error {
	existing, err := GetUser(id)
	if err != nil {
		return err
	}
	// We can't remove the last account that keeps a privileged role.
	switch existing.Role.Slug {
	case RoleProviderAdmin:
		if err = EnsureEnoughProviderAdmins(); err != nil {
			return err
		}
	case RoleAdmin:
		if err = EnsureEnoughAdmins(); err != nil {
			return err
		}
	}
	// The user's personal team is private to them, so it goes with the user
	// (refusing if it still holds objects). This runs first so nothing is
	// half-deleted if the personal team isn't empty.
	if err = DeletePersonalTeam(id); err != nil {
		return err
	}
	// The user's shared-team campaigns, templates, and other objects belong to
	// their teams, so they stay for the rest of the team. Only the user's own
	// memberships go.
	err = db.Where("user_id=?", id).Delete(&TeamMember{}).Error
	if err != nil {
		return err
	}
	err = db.Where("user_id=?", id).Delete(&RecoveryCode{}).Error
	if err != nil {
		return err
	}
	err = DeleteUserAPIKeys(id)
	if err != nil {
		return err
	}
	// Sign the user out everywhere
	err = DeleteUserSessions(id, "")
	if err != nil {
		return err
	}
	// Finally, delete the user
	err = db.Where("id=?", id).Delete(&User{}).Error
	return err
}
