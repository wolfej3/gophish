package models

import (
	"strings"
	"time"

	"gopkg.in/check.v1"
)

func (s *ModelsSuite) TestCreateAPIKeyValidation(c *check.C) {
	_, err := CreateAPIKey(&APIKey{UserId: 1, Name: " ", Access: APIKeyAccessRead}, nil)
	c.Assert(err, check.Equals, ErrAPIKeyName)
	_, err = CreateAPIKey(&APIKey{UserId: 1, Name: "x", Access: "admin"}, nil)
	c.Assert(err, check.Equals, ErrAPIKeyAccess)
	_, err = CreateAPIKey(&APIKey{UserId: 1, Name: "x", Access: APIKeyAccessRead}, []string{"api_keys"})
	c.Assert(err, check.Equals, ErrAPIKeyResource)
}

func (s *ModelsSuite) TestAPIKeyLifecycle(c *check.C) {
	k := APIKey{UserId: 1, Name: "Reporting", Access: APIKeyAccessRead}
	key, err := CreateAPIKey(&k, []string{"groups", "campaigns", "groups"})
	c.Assert(err, check.Equals, nil)
	c.Assert(strings.HasPrefix(key, ScopedAPIKeyPrefix), check.Equals, true)
	c.Assert(strings.HasPrefix(key, k.Prefix), check.Equals, true)
	c.Assert(k.Resources, check.Equals, "campaigns,groups")
	// Only the hash is stored
	c.Assert(strings.Contains(k.KeyHash, key), check.Equals, false)

	got, err := GetAPIKeyByKey(key)
	c.Assert(err, check.Equals, nil)
	c.Assert(got.Id, check.Equals, k.Id)
	c.Assert(got.LastUsedAt, check.NotNil)
	_, err = GetAPIKeyByKey(key + "x")
	c.Assert(err, check.NotNil)

	// Keys can only be deleted by their owner
	c.Assert(DeleteAPIKey(k.Id, 2), check.NotNil)
	c.Assert(DeleteAPIKey(k.Id, 1), check.Equals, nil)
	_, err = GetAPIKeyByKey(key)
	c.Assert(err, check.NotNil)
}

func (s *ModelsSuite) TestExpiredAPIKey(c *check.C) {
	past := time.Now().Add(-time.Minute)
	k := APIKey{UserId: 1, Name: "Old", Access: APIKeyAccessRead, ExpiresAt: &past}
	key, err := CreateAPIKey(&k, nil)
	c.Assert(err, check.Equals, nil)
	_, err = GetAPIKeyByKey(key)
	c.Assert(err, check.NotNil)
}

func (s *ModelsSuite) TestAPIKeyAllows(c *check.C) {
	read := APIKey{Access: APIKeyAccessRead, Resources: APIKeyAllResources}
	c.Assert(read.Allows("campaigns", false), check.Equals, true)
	c.Assert(read.Allows("campaigns", true), check.Equals, false)
	// Resources that aren't listed, like key management, are never allowed
	c.Assert(read.Allows("api_keys", false), check.Equals, false)
	c.Assert(read.Allows("reset", false), check.Equals, false)

	write := APIKey{Access: APIKeyAccessWrite, Resources: "groups,pages"}
	c.Assert(write.Allows("groups", true), check.Equals, true)
	c.Assert(write.Allows("pages", false), check.Equals, true)
	c.Assert(write.Allows("campaigns", false), check.Equals, false)
	c.Assert(write.Allows("group", false), check.Equals, false)
}
