package models

import (
	"gopkg.in/check.v1"
	"gorm.io/gorm"
)

// defaultTeamID returns the Default team's ID, created at setup.
func (s *ModelsSuite) defaultTeamID(ch *check.C) int64 {
	t, err := GetDefaultTeam()
	ch.Assert(err, check.Equals, nil)
	return t.Id
}

func (s *ModelsSuite) TestDefaultTeamHasAdmin(ch *check.C) {
	admin, err := GetUser(1)
	ch.Assert(err, check.Equals, nil)
	teams, err := GetUserTeams(admin)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(teams) >= 1, check.Equals, true)
}

func (s *ModelsSuite) TestTeamCreateValidation(ch *check.C) {
	ch.Assert(PostTeam(&Team{Name: "  "}), check.Equals, ErrTeamName)
	ch.Assert(PostTeam(&Team{Name: "Blue"}), check.Equals, nil)
	ch.Assert(PostTeam(&Team{Name: "Blue"}), check.Equals, ErrTeamNameTaken)
}

func (s *ModelsSuite) TestTeamMembershipAndRole(ch *check.C) {
	team := Team{Name: "Red"}
	ch.Assert(PostTeam(&team), check.Equals, nil)
	user := User{Username: "viewer@example.com", Hash: "x", ApiKey: "k-viewer"}
	ch.Assert(PutUser(&user), check.Equals, nil)

	// Not a member yet
	_, err := GetUserTeamRole(user, team.Id)
	ch.Assert(err, check.Equals, gorm.ErrRecordNotFound)

	ch.Assert(SetTeamMember(team.Id, user.Id, "owner"), check.Equals, ErrTeamRole)
	ch.Assert(SetTeamMember(team.Id, user.Id, TeamRoleViewer), check.Equals, nil)
	role, err := GetUserTeamRole(user, team.Id)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(role, check.Equals, TeamRoleViewer)

	// Changing the role replaces it, not duplicates it
	ch.Assert(SetTeamMember(team.Id, user.Id, TeamRoleEditor), check.Equals, nil)
	members, err := GetTeamMembers(team.Id)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(members), check.Equals, 1)
	ch.Assert(members[0].Role, check.Equals, TeamRoleEditor)
	ch.Assert(members[0].Username, check.Equals, "viewer@example.com")

	ch.Assert(RemoveTeamMember(team.Id, user.Id), check.Equals, nil)
	_, err = GetUserTeamRole(user, team.Id)
	ch.Assert(err, check.Equals, gorm.ErrRecordNotFound)
}

func (s *ModelsSuite) TestAdminSeesAllTeams(ch *check.C) {
	ch.Assert(PostTeam(&Team{Name: "Green"}), check.Equals, nil)
	admin, _ := GetUser(1)
	teams, err := GetUserTeams(admin)
	ch.Assert(err, check.Equals, nil)
	// Admin is an editor on every team without being a member
	found := false
	for _, t := range teams {
		if t.Name == "Green" {
			ch.Assert(t.Role, check.Equals, TeamRoleEditor)
			found = true
		}
	}
	ch.Assert(found, check.Equals, true)
}

func (s *ModelsSuite) TestObjectsScopedByTeam(ch *check.C) {
	teamA := s.defaultTeamID(ch)
	teamB := Team{Name: "Other"}
	ch.Assert(PostTeam(&teamB), check.Equals, nil)

	a := Group{Name: "A Group", TeamId: teamA, UserId: 1}
	a.Targets = []Target{{BaseRecipient: BaseRecipient{Email: "a@example.com"}}}
	ch.Assert(PostGroup(&a), check.Equals, nil)
	b := Group{Name: "B Group", TeamId: teamB.Id, UserId: 1}
	b.Targets = []Target{{BaseRecipient: BaseRecipient{Email: "b@example.com"}}}
	ch.Assert(PostGroup(&b), check.Equals, nil)

	as, err := GetGroups(teamA)
	ch.Assert(err, check.Equals, nil)
	ch.Assert(len(as), check.Equals, 1)
	ch.Assert(as[0].Name, check.Equals, "A Group")

	// Team A can't fetch team B's group by id
	_, err = GetGroup(b.Id, teamA)
	ch.Assert(err, check.Equals, gorm.ErrRecordNotFound)
	_, err = GetGroup(b.Id, teamB.Id)
	ch.Assert(err, check.Equals, nil)
}

func (s *ModelsSuite) TestDeleteTeamRules(ch *check.C) {
	// Can't delete the only team
	ch.Assert(DeleteTeam(s.defaultTeamID(ch)), check.Equals, ErrLastTeam)

	team := Team{Name: "Temp"}
	ch.Assert(PostTeam(&team), check.Equals, nil)
	g := Group{Name: "Held", TeamId: team.Id, UserId: 1}
	g.Targets = []Target{{BaseRecipient: BaseRecipient{Email: "h@example.com"}}}
	ch.Assert(PostGroup(&g), check.Equals, nil)

	// Can't delete a team that still owns objects
	ch.Assert(DeleteTeam(team.Id), check.Equals, ErrTeamNotEmpty)
	ch.Assert(DeleteGroup(&g), check.Equals, nil)
	ch.Assert(DeleteTeam(team.Id), check.Equals, nil)
}
