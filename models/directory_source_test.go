package models

import (
	"fmt"
	"time"

	check "gopkg.in/check.v1"
)

// makeSyncGroup creates a uniquely-named group and returns its id, for
// attaching a directory source.
func (s *ModelsSuite) makeSyncGroup(c *check.C) int64 {
	g := Group{Name: fmt.Sprintf("Sync Group %d", time.Now().UnixNano())}
	g.UserId = 1
	g.TeamId = 1
	g.Targets = []Target{
		{BaseRecipient: BaseRecipient{Email: "member@example.com", FirstName: "Sync", LastName: "Member"}},
	}
	c.Assert(PostGroup(&g), check.Equals, nil)
	return g.Id
}

func (s *ModelsSuite) TestDirectorySourceSyncIntervalPersists(c *check.C) {
	gid := s.makeSyncGroup(c)
	src := &DirectorySource{
		GroupId:      gid,
		Connector:    "entra",
		ExternalID:   "group-123",
		SyncInterval: 60,
	}
	c.Assert(SetDirectorySource(src), check.Equals, nil)

	got, err := GetDirectorySource(gid)
	c.Assert(err, check.Equals, nil)
	c.Assert(got.SyncInterval, check.Equals, int64(60))
}

func (s *ModelsSuite) TestGetDueDirectorySources(c *check.C) {
	now := time.Now().UTC()

	// On-demand only (interval 0) is never due.
	manualGid := s.makeSyncGroup(c)
	c.Assert(SetDirectorySource(&DirectorySource{
		GroupId: manualGid, Connector: "entra", ExternalID: "manual", SyncInterval: 0,
	}), check.Equals, nil)

	// Never synced with an interval set -> due.
	freshGid := s.makeSyncGroup(c)
	c.Assert(SetDirectorySource(&DirectorySource{
		GroupId: freshGid, Connector: "entra", ExternalID: "fresh", SyncInterval: 60,
	}), check.Equals, nil)

	// Synced long ago relative to its interval -> due.
	staleGid := s.makeSyncGroup(c)
	c.Assert(SetDirectorySource(&DirectorySource{
		GroupId: staleGid, Connector: "entra", ExternalID: "stale", SyncInterval: 60,
	}), check.Equals, nil)
	stale := now.Add(-2 * time.Hour)
	c.Assert(db.Model(&DirectorySource{}).Where("group_id=?", staleGid).
		Update("last_synced_at", stale).Error, check.Equals, nil)

	// Synced recently relative to its interval -> not due.
	recentGid := s.makeSyncGroup(c)
	c.Assert(SetDirectorySource(&DirectorySource{
		GroupId: recentGid, Connector: "entra", ExternalID: "recent", SyncInterval: 60,
	}), check.Equals, nil)
	recent := now.Add(-5 * time.Minute)
	c.Assert(db.Model(&DirectorySource{}).Where("group_id=?", recentGid).
		Update("last_synced_at", recent).Error, check.Equals, nil)

	due, err := GetDueDirectorySources(now)
	c.Assert(err, check.Equals, nil)

	dueIDs := map[int64]bool{}
	for _, d := range due {
		dueIDs[d.GroupId] = true
	}
	c.Assert(dueIDs[freshGid], check.Equals, true)
	c.Assert(dueIDs[staleGid], check.Equals, true)
	c.Assert(dueIDs[recentGid], check.Equals, false)
	c.Assert(dueIDs[manualGid], check.Equals, false)
}
