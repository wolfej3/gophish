package models

import (
	"gopkg.in/check.v1"
	"gorm.io/gorm"
)

// makeTenant creates a tenant for a test.
func (s *ModelsSuite) makeTenant(c *check.C, name string) Tenant {
	t := Tenant{Name: name}
	c.Assert(PostTenant(&t), check.Equals, nil)
	return t
}

// makeUser creates a user with the given role in the given tenant.
func (s *ModelsSuite) makeUser(c *check.C, username, roleSlug string, tenantID int64) User {
	role, err := GetRoleBySlug(roleSlug)
	c.Assert(err, check.Equals, nil)
	u := User{
		Username: username,
		ApiKey:   username + "-apikey",
		Role:     role,
		RoleID:   role.ID,
		TenantId: tenantID,
	}
	c.Assert(db.Omit("Role").Save(&u).Error, check.Equals, nil)
	return u
}

// containsTeam reports whether the membership list includes the team id.
func containsTeam(teams []TeamMembership, id int64) bool {
	for _, t := range teams {
		if t.Id == id {
			return true
		}
	}
	return false
}

func (s *ModelsSuite) TestTenantValidation(c *check.C) {
	empty := Tenant{Name: "   "}
	c.Assert(PostTenant(&empty), check.Equals, ErrTenantName)

	s.makeTenant(c, "Acme")
	dup := Tenant{Name: "Acme"}
	c.Assert(PostTenant(&dup), check.Equals, ErrTenantNameTaken)
}

func (s *ModelsSuite) TestTenantCRUD(c *check.C) {
	t := s.makeTenant(c, "Acme")
	c.Assert(t.Id > 0, check.Equals, true)

	// PostTenant seeds a single shared default team.
	teams, err := GetSharedTeamsByTenant(t.Id)
	c.Assert(err, check.Equals, nil)
	c.Assert(len(teams), check.Equals, 1)

	// Rename.
	t.Name = "Acme Corp"
	c.Assert(PutTenant(&t), check.Equals, nil)
	got, err := GetTenant(t.Id)
	c.Assert(err, check.Equals, nil)
	c.Assert(got.Name, check.Equals, "Acme Corp")

	// A tenant with a shared team can't be deleted until the team is gone.
	c.Assert(DeleteTenant(t.Id), check.Equals, ErrTenantNotEmpty)
	for _, tm := range teams {
		c.Assert(DeleteTeam(tm.Id), check.Equals, nil)
	}
	c.Assert(DeleteTenant(t.Id), check.Equals, nil)

	// The last remaining tenant (Default) can't be deleted.
	def, err := GetDefaultTenant()
	c.Assert(err, check.Equals, nil)
	c.Assert(DeleteTenant(def.Id), check.Equals, ErrLastTenant)
}

func (s *ModelsSuite) TestTenantBlocksDeleteWithUsers(c *check.C) {
	t := s.makeTenant(c, "Acme")
	// Remove the seeded team so only the user blocks deletion.
	teams, err := GetSharedTeamsByTenant(t.Id)
	c.Assert(err, check.Equals, nil)
	for _, tm := range teams {
		c.Assert(DeleteTeam(tm.Id), check.Equals, nil)
	}
	s.makeUser(c, "acme-user", RoleUser, t.Id)
	c.Assert(DeleteTenant(t.Id), check.Equals, ErrTenantNotEmpty)
}

func (s *ModelsSuite) TestPersonalTeam(c *check.C) {
	acme := s.makeTenant(c, "Acme")
	u := s.makeUser(c, "p", RoleUser, acme.Id)

	t1, err := EnsurePersonalTeam(u)
	c.Assert(err, check.Equals, nil)
	c.Assert(t1.IsPersonal(), check.Equals, true)
	c.Assert(t1.TenantId, check.Equals, acme.Id)

	// It's idempotent: a second call returns the same team.
	t2, err := EnsurePersonalTeam(u)
	c.Assert(err, check.Equals, nil)
	c.Assert(t2.Id, check.Equals, t1.Id)

	// The owner is an editor of their personal team.
	role, err := GetUserTeamRole(u, t1.Id)
	c.Assert(err, check.Equals, nil)
	c.Assert(role, check.Equals, TeamRoleEditor)

	// You can't add members to, or directly delete, a personal team.
	other := s.makeUser(c, "other", RoleUser, acme.Id)
	c.Assert(SetTeamMember(t1.Id, other.Id, TeamRoleViewer), check.Equals, ErrPersonalTeam)
	c.Assert(DeleteTeam(t1.Id), check.Equals, ErrPersonalTeam)

	// A personal team with objects can't be deleted until it's cleared.
	tmpl := Template{Name: "Private template", UserId: u.Id, TeamId: t1.Id, Subject: "s", Text: "t", HTML: "h"}
	c.Assert(PostTemplate(&tmpl), check.Equals, nil)
	c.Assert(DeletePersonalTeam(u.Id), check.Equals, ErrPersonalTeamNotEmpty)
	c.Assert(DeleteTemplate(tmpl.Id, t1.Id), check.Equals, nil)

	// Once empty, it's removed.
	c.Assert(DeletePersonalTeam(u.Id), check.Equals, nil)
	_, err = GetTeam(t1.Id)
	c.Assert(err, check.Equals, gorm.ErrRecordNotFound)
}

func (s *ModelsSuite) TestGetUserTeamsScoping(c *check.C) {
	acme := s.makeTenant(c, "Acme")
	globex := s.makeTenant(c, "Globex")

	acmeTeam := Team{Name: "Acme Red", TenantId: acme.Id}
	c.Assert(PostTeam(&acmeTeam), check.Equals, nil)
	globexTeam := Team{Name: "Globex Blue", TenantId: globex.Id}
	c.Assert(PostTeam(&globexTeam), check.Equals, nil)

	acmeAdmin := s.makeUser(c, "acme-admin", RoleAdmin, acme.Id)
	_, err := EnsurePersonalTeam(acmeAdmin)
	c.Assert(err, check.Equals, nil)

	acmeUser := s.makeUser(c, "acme-user", RoleUser, acme.Id)
	acmeUserPersonal, err := EnsurePersonalTeam(acmeUser)
	c.Assert(err, check.Equals, nil)

	peer := s.makeUser(c, "acme-peer", RoleUser, acme.Id)
	peerPersonal, err := EnsurePersonalTeam(peer)
	c.Assert(err, check.Equals, nil)

	// A tenant admin sees every team in their own tenant (shared and personal,
	// including a peer's personal team) and nothing from another tenant.
	adminTeams, err := GetUserTeams(acmeAdmin)
	c.Assert(err, check.Equals, nil)
	for _, t := range adminTeams {
		c.Assert(t.TenantId, check.Equals, acme.Id)
	}
	c.Assert(containsTeam(adminTeams, acmeTeam.Id), check.Equals, true)
	c.Assert(containsTeam(adminTeams, acmeUserPersonal.Id), check.Equals, true)
	c.Assert(containsTeam(adminTeams, globexTeam.Id), check.Equals, false)

	// A regular user sees only their own teams: their personal team, not a
	// peer's, and not shared teams they aren't a member of.
	userTeams, err := GetUserTeams(acmeUser)
	c.Assert(err, check.Equals, nil)
	c.Assert(containsTeam(userTeams, acmeUserPersonal.Id), check.Equals, true)
	c.Assert(containsTeam(userTeams, peerPersonal.Id), check.Equals, false)
	c.Assert(containsTeam(userTeams, acmeTeam.Id), check.Equals, false)

	// The provider admin (user 1) sees teams across every tenant.
	provider, err := GetUser(1)
	c.Assert(err, check.Equals, nil)
	provTeams, err := GetUserTeams(provider)
	c.Assert(err, check.Equals, nil)
	c.Assert(containsTeam(provTeams, acmeTeam.Id), check.Equals, true)
	c.Assert(containsTeam(provTeams, globexTeam.Id), check.Equals, true)
	c.Assert(containsTeam(provTeams, peerPersonal.Id), check.Equals, true)
}

func (s *ModelsSuite) TestProviderAdminGuard(c *check.C) {
	// User 1 is the only provider admin, so it can't be removed.
	c.Assert(EnsureEnoughProviderAdmins(), check.Equals, ErrModifyingOnlyProviderAdmin)
	c.Assert(DeleteUser(1), check.Equals, ErrModifyingOnlyProviderAdmin)

	// With a second provider admin, the guard passes.
	def, err := GetDefaultTenant()
	c.Assert(err, check.Equals, nil)
	s.makeUser(c, "prov2", RoleProviderAdmin, def.Id)
	c.Assert(EnsureEnoughProviderAdmins(), check.Equals, nil)
}
