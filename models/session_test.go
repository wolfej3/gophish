package models

import (
	"time"

	"gopkg.in/check.v1"
	"gorm.io/gorm"
)

func (s *ModelsSuite) TestSessionLifecycle(c *check.C) {
	uid := int64(1)
	err := PutSession("token", &uid, []byte("data"), time.Now().Add(time.Hour))
	c.Assert(err, check.Equals, nil)
	got, err := GetSessionByToken("token")
	c.Assert(err, check.Equals, nil)
	c.Assert(*got.UserId, check.Equals, uid)
	c.Assert(string(got.Data), check.Equals, "data")
	// Only a hash of the token is stored
	c.Assert(got.TokenHash, check.Not(check.Equals), "token")

	// Saving again updates the existing session rather than adding one
	err = PutSession("token", &uid, []byte("updated"), time.Now().Add(time.Hour))
	c.Assert(err, check.Equals, nil)
	var count int64
	c.Assert(db.Model(&Session{}).Count(&count).Error, check.Equals, nil)
	c.Assert(count, check.Equals, int64(1))

	c.Assert(DeleteSessionByToken("token"), check.Equals, nil)
	_, err = GetSessionByToken("token")
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
}

func (s *ModelsSuite) TestExpiredSessions(c *check.C) {
	uid := int64(1)
	err := PutSession("expired", &uid, nil, time.Now().Add(-time.Minute))
	c.Assert(err, check.Equals, nil)
	err = PutSession("current", &uid, nil, time.Now().Add(time.Hour))
	c.Assert(err, check.Equals, nil)

	_, err = GetSessionByToken("expired")
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)

	c.Assert(DeleteExpiredSessions(), check.Equals, nil)
	var count int64
	c.Assert(db.Model(&Session{}).Count(&count).Error, check.Equals, nil)
	c.Assert(count, check.Equals, int64(1))
	_, err = GetSessionByToken("current")
	c.Assert(err, check.Equals, nil)
}

func (s *ModelsSuite) TestDeleteUserSessions(c *check.C) {
	uid, other := int64(1), int64(2)
	expires := time.Now().Add(time.Hour)
	for _, token := range []string{"keep", "revoke"} {
		c.Assert(PutSession(token, &uid, nil, expires), check.Equals, nil)
	}
	c.Assert(PutSession("other-user", &other, nil, expires), check.Equals, nil)

	c.Assert(DeleteUserSessions(uid, "keep"), check.Equals, nil)
	_, err := GetSessionByToken("keep")
	c.Assert(err, check.Equals, nil)
	_, err = GetSessionByToken("revoke")
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetSessionByToken("other-user")
	c.Assert(err, check.Equals, nil)

	c.Assert(DeleteUserSessions(uid, ""), check.Equals, nil)
	_, err = GetSessionByToken("keep")
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
}
