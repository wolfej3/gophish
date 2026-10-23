package models

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/gophish/gophish/auth"
	"gorm.io/gorm"
)

// ScopedAPIKeyPrefix starts every scoped API key, which tells them apart from
// users' full-access API keys.
const ScopedAPIKeyPrefix = "gpk_"

// APIKeyResources are the parts of the API a scoped key can be given access
// to. Each is the first segment of an API path, e.g. /api/campaigns/.
var APIKeyResources = []string{
	"audit_log",
	"campaigns",
	"groups",
	"imap",
	"import",
	"pages",
	"smtp",
	"templates",
	"users",
	"util",
	"webhooks",
}

// Access levels for scoped API keys. Write access includes read access.
const (
	APIKeyAccessRead  = "read"
	APIKeyAccessWrite = "write"
)

// APIKeyAllResources gives a scoped key access to every resource.
const APIKeyAllResources = "*"

// ErrAPIKeyName is returned when a scoped API key has no name.
var ErrAPIKeyName = errors.New("API key name is required")

// ErrAPIKeyAccess is returned when a scoped API key's access level isn't
// "read" or "write".
var ErrAPIKeyAccess = errors.New(`API key access must be "read" or "write"`)

// ErrAPIKeyResource is returned when a scoped API key names a resource that
// doesn't exist.
var ErrAPIKeyResource = errors.New("Unknown API key resource")

// APIKey is an API key with limited access, which a user creates for a
// script or integration. Its access is also limited by its owner's role.
// Only a hash of the key is stored; the key itself is shown once, when it's
// created.
type APIKey struct {
	Id     int64  `json:"id"`
	UserId int64  `json:"-"`
	Name   string `json:"name"`
	// Prefix is the start of the key, so that users can tell keys apart.
	Prefix  string `json:"prefix"`
	KeyHash string `json:"-"`
	Access  string `json:"access"`
	// Resources holds the resources the key can use, separated by commas,
	// or "*" for all of them.
	Resources  string     `json:"resources"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// hashAPIKey returns the hex-encoded SHA-256 hash of an API key. Keys are
// random, so a fast hash is sufficient.
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// normalizeResources validates the resources and returns them in the form
// they're stored.
func normalizeResources(resources []string) (string, error) {
	if len(resources) == 0 {
		return APIKeyAllResources, nil
	}
	seen := map[string]bool{}
	for _, r := range resources {
		if r == APIKeyAllResources {
			return APIKeyAllResources, nil
		}
		i := sort.SearchStrings(APIKeyResources, r)
		if i == len(APIKeyResources) || APIKeyResources[i] != r {
			return "", ErrAPIKeyResource
		}
		seen[r] = true
	}
	list := make([]string, 0, len(seen))
	for r := range seen {
		list = append(list, r)
	}
	sort.Strings(list)
	return strings.Join(list, ","), nil
}

// CreateAPIKey creates a scoped API key for the user. It returns the key,
// which isn't stored and can't be retrieved later.
func CreateAPIKey(k *APIKey, resources []string) (string, error) {
	k.Name = strings.TrimSpace(k.Name)
	if k.Name == "" {
		return "", ErrAPIKeyName
	}
	if k.Access != APIKeyAccessRead && k.Access != APIKeyAccessWrite {
		return "", ErrAPIKeyAccess
	}
	var err error
	k.Resources, err = normalizeResources(resources)
	if err != nil {
		return "", err
	}
	key := ScopedAPIKeyPrefix + auth.GenerateSecureKey(auth.APIKeyLength)
	k.Id = 0
	k.Prefix = key[:len(ScopedAPIKeyPrefix)+8]
	k.KeyHash = hashAPIKey(key)
	k.CreatedAt = time.Now().UTC()
	k.LastUsedAt = nil
	if k.ExpiresAt != nil {
		t := k.ExpiresAt.UTC()
		k.ExpiresAt = &t
	}
	return key, db.Create(k).Error
}

// GetAPIKeys returns the user's scoped API keys.
func GetAPIKeys(uid int64) ([]APIKey, error) {
	keys := []APIKey{}
	err := db.Where("user_id = ?", uid).Order("id").Find(&keys).Error
	return keys, err
}

// DeleteAPIKey deletes one of the user's scoped API keys.
func DeleteAPIKey(id, uid int64) error {
	result := db.Where("id = ? AND user_id = ?", id, uid).Delete(&APIKey{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetAPIKeyByKey returns the unexpired scoped API key matching key, and
// records that it was used.
func GetAPIKeyByKey(key string) (APIKey, error) {
	k := APIKey{}
	err := db.Where("key_hash = ?", hashAPIKey(key)).Take(&k).Error
	if err != nil {
		return k, err
	}
	now := time.Now().UTC()
	if k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
		return k, gorm.ErrRecordNotFound
	}
	k.LastUsedAt = &now
	err = db.Model(&APIKey{}).Where("id = ?", k.Id).Update("last_used_at", now).Error
	return k, err
}

// Allows returns whether the key may make a request to the resource. Reads
// need read access; anything else needs write access.
func (k APIKey) Allows(resource string, write bool) bool {
	if write && k.Access != APIKeyAccessWrite {
		return false
	}
	if k.Resources == APIKeyAllResources {
		i := sort.SearchStrings(APIKeyResources, resource)
		return i < len(APIKeyResources) && APIKeyResources[i] == resource
	}
	for _, r := range strings.Split(k.Resources, ",") {
		if r == resource {
			return true
		}
	}
	return false
}

// DeleteUserAPIKeys deletes all of the user's scoped API keys.
func DeleteUserAPIKeys(uid int64) error {
	return db.Where("user_id = ?", uid).Delete(&APIKey{}).Error
}
