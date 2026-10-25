package models

import (
	"gopkg.in/check.v1"
	"gorm.io/gorm"
)

func (s *ModelsSuite) TestGetUserExists(c *check.C) {
	u, err := GetUser(1)
	c.Assert(err, check.Equals, nil)
	c.Assert(u.Username, check.Equals, "admin")
}

func (s *ModelsSuite) TestGetUserByUsernameWithExistingUser(c *check.C) {
	u, err := GetUserByUsername("admin")
	c.Assert(err, check.Equals, nil)
	c.Assert(u.Username, check.Equals, "admin")
}

func (s *ModelsSuite) TestGetUserDoesNotExist(c *check.C) {
	u, err := GetUser(100)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	c.Assert(u.Username, check.Equals, "")
}

func (s *ModelsSuite) TestGetUserByAPIKeyWithExistingAPIKey(c *check.C) {
	u, err := GetUser(1)
	c.Assert(err, check.Equals, nil)

	got, err := GetUserByAPIKey(u.ApiKey)
	c.Assert(err, check.Equals, nil)
	c.Assert(got.Id, check.Equals, u.Id)
}

func (s *ModelsSuite) TestGetUserByAPIKeyWithNotExistingAPIKey(c *check.C) {
	u, err := GetUser(1)
	c.Assert(err, check.Equals, nil)

	u, err = GetUserByAPIKey(u.ApiKey + "test")
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	c.Assert(u.Username, check.Equals, "")
}

func (s *ModelsSuite) TestGetUserByUsernameWithNotExistingUser(c *check.C) {
	u, err := GetUserByUsername("test user does not exist")
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	c.Assert(u.Username, check.Equals, "")
}

func (s *ModelsSuite) TestPutUser(c *check.C) {
	u, _ := GetUser(1)
	u.Username = "admin_changed"
	err := PutUser(&u)
	c.Assert(err, check.Equals, nil)
	u, err = GetUser(1)
	c.Assert(err, check.Equals, nil)
	c.Assert(u.Username, check.Equals, "admin_changed")
}

func (s *ModelsSuite) TestGeneratedAPIKey(c *check.C) {
	u, err := GetUser(1)
	c.Assert(err, check.Equals, nil)
	c.Assert(u.ApiKey, check.Not(check.Equals), "12345678901234567890123456789012")
}

func (s *ModelsSuite) verifyRoleCount(c *check.C, roleID, expected int64) {
	var adminCount int64
	err := db.Model(&User{}).Where("role_id=?", roleID).Count(&adminCount).Error
	c.Assert(err, check.Equals, nil)
	c.Assert(adminCount, check.Equals, expected)
}

func (s *ModelsSuite) TestDeleteLastAdmin(c *check.C) {
	role, err := GetRoleBySlug(RoleAdmin)
	c.Assert(err, check.Equals, nil)
	// User 1 is the provider admin, so there are no tenant admins to start.
	s.verifyRoleCount(c, role.ID, 0)

	admin1 := User{Username: "admin-1", Hash: "123456", ApiKey: "key-admin-1", Role: role, RoleID: role.ID}
	c.Assert(PutUser(&admin1), check.Equals, nil)
	admin2 := User{Username: "admin-2", Hash: "123456", ApiKey: "key-admin-2", Role: role, RoleID: role.ID}
	c.Assert(PutUser(&admin2), check.Equals, nil)
	s.verifyRoleCount(c, role.ID, 2)

	// Deleting one admin works since another remains.
	c.Assert(DeleteUser(admin2.Id), check.Equals, nil)
	s.verifyRoleCount(c, role.ID, 1)

	// Deleting the last admin is refused.
	c.Assert(DeleteUser(admin1.Id), check.Equals, ErrModifyingOnlyAdmin)
	s.verifyRoleCount(c, role.ID, 1)
}
