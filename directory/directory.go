// Package directory syncs the members of a Gophish group from a group in an
// identity directory, such as Microsoft Entra ID or Google Workspace.
package directory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
)

// syncTimeout bounds how long a sync can take.
const syncTimeout = 2 * time.Minute

// Provider fetches the members of a directory group.
type Provider interface {
	// Members returns the people in the directory group, as recipients ready
	// to be stored as group targets.
	Members(ctx context.Context, externalID string) ([]models.BaseRecipient, error)
}

// ErrUnknownProvider is returned when a connector names a provider that isn't
// supported.
var ErrUnknownProvider = errors.New("Unknown directory provider")

// newProvider returns the provider for the connector.
func newProvider(conn config.DirectoryConnector) (Provider, error) {
	switch conn.Provider {
	case "msgraph":
		return newMSGraphProvider(conn)
	case "google":
		return newGoogleProvider(conn)
	case "ldap", "ad", "activedirectory":
		return newLDAPProvider(conn)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, conn.Provider)
	}
}

// FetchMembers returns the members of the directory group named by externalID
// through the connector.
func FetchMembers(ctx context.Context, conn config.DirectoryConnector, externalID string) ([]models.BaseRecipient, error) {
	provider, err := newProvider(conn)
	if err != nil {
		return nil, err
	}
	return provider.Members(ctx, externalID)
}

// SyncGroup replaces the group's targets with the members of its directory
// group and records the outcome on the source. The caller is responsible for
// having loaded the group for the right team. An empty directory group is
// treated as an error rather than emptying the Gophish group, so a
// misconfiguration doesn't wipe a campaign's recipients.
func SyncGroup(conn config.DirectoryConnector, source models.DirectorySource, group *models.Group) error {
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	err := syncGroup(ctx, conn, source, group)
	if recordErr := models.RecordDirectorySync(source.GroupId, err); recordErr != nil && err == nil {
		return recordErr
	}
	return err
}

func syncGroup(ctx context.Context, conn config.DirectoryConnector, source models.DirectorySource, group *models.Group) error {
	members, err := FetchMembers(ctx, conn, source.ExternalID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return errors.New("the directory group has no members with an email address")
	}
	group.Targets = make([]models.Target, 0, len(members))
	for _, m := range members {
		group.Targets = append(group.Targets, models.Target{BaseRecipient: m})
	}
	return models.PutGroup(group)
}
