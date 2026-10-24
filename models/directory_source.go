package models

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrDirectoryConnectorRequired is returned when a source has no connector.
var ErrDirectoryConnectorRequired = errors.New("A directory connector is required")

// ErrDirectoryExternalIDRequired is returned when a source names no directory
// group.
var ErrDirectoryExternalIDRequired = errors.New("A directory group is required")

// DirectorySource links a Gophish group to a group in an identity directory,
// so its members can be synced. The connector names a directory connector
// configured in config.json; the credentials themselves aren't stored here.
type DirectorySource struct {
	GroupId      int64      `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Connector    string     `json:"connector"`
	ExternalID   string     `json:"external_id"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
	LastError    string     `json:"last_error"`
}

// TableName sets the table for DirectorySource.
func (DirectorySource) TableName() string {
	return "directory_sources"
}

// GetDirectorySource returns the directory source for the group, or
// gorm.ErrRecordNotFound if it doesn't sync from a directory.
func GetDirectorySource(groupID int64) (DirectorySource, error) {
	s := DirectorySource{}
	err := db.Where("group_id = ?", groupID).Take(&s).Error
	return s, err
}

// SetDirectorySource creates or replaces a group's directory source.
func SetDirectorySource(s *DirectorySource) error {
	if s.Connector == "" {
		return ErrDirectoryConnectorRequired
	}
	if s.ExternalID == "" {
		return ErrDirectoryExternalIDRequired
	}
	existing := DirectorySource{}
	err := db.Where("group_id = ?", s.GroupId).Take(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// Keep the sync history when the connector or group changes.
	s.LastSyncedAt = existing.LastSyncedAt
	s.LastError = existing.LastError
	return db.Save(s).Error
}

// DeleteDirectorySource removes a group's directory source, if it has one.
func DeleteDirectorySource(groupID int64) error {
	return db.Where("group_id = ?", groupID).Delete(&DirectorySource{}).Error
}

// RecordDirectorySync stores the outcome of a sync attempt.
func RecordDirectorySync(groupID int64, syncErr error) error {
	now := time.Now().UTC()
	updates := map[string]interface{}{"last_synced_at": now, "last_error": ""}
	if syncErr != nil {
		updates["last_error"] = syncErr.Error()
	}
	return db.Model(&DirectorySource{}).Where("group_id = ?", groupID).Updates(updates).Error
}
