package models

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Team roles. Editors can create and change the team's objects; viewers can
// only look at them. Users with the PermissionModifySystem permission can
// edit every team's objects without being members.
const (
	TeamRoleEditor = "editor"
	TeamRoleViewer = "viewer"
)

// DefaultTeamName is the team that holds everything created before teams
// existed. New users join it unless they're given other teams.
const DefaultTeamName = "Default"

// ErrTeamName is returned when a team has no name.
var ErrTeamName = errors.New("Team name is required")

// ErrTeamNameTaken is returned when another team already has the name.
var ErrTeamNameTaken = errors.New("A team with that name already exists")

// ErrTeamRole is returned when a membership's role isn't "editor" or
// "viewer".
var ErrTeamRole = errors.New(`Team role must be "editor" or "viewer"`)

// ErrTeamNotEmpty is returned when deleting a team that still has objects.
var ErrTeamNotEmpty = errors.New("Move or delete the team's campaigns, templates, landing pages, groups, and sending profiles before deleting it")

// ErrLastTeam is returned when deleting the only team.
var ErrLastTeam = errors.New("Cannot delete the only team")

// Team is a group of users who share campaigns, templates, landing pages,
// groups, and sending profiles.
type Team struct {
	Id        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TeamMember gives a user a role on a team.
type TeamMember struct {
	TeamId int64  `json:"team_id" gorm:"primaryKey;autoIncrement:false"`
	UserId int64  `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Role   string `json:"role"`
}

// TeamMembership is a team and the role a user has on it.
type TeamMembership struct {
	Team
	Role string `json:"role"`
}

// TeamMemberDetail is a member of a team with their username.
type TeamMemberDetail struct {
	UserId   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// validTeamRole returns whether the role is one a member can have.
func validTeamRole(role string) bool {
	return role == TeamRoleEditor || role == TeamRoleViewer
}

// GetTeams returns every team, ordered by name.
func GetTeams() ([]Team, error) {
	teams := []Team{}
	err := db.Order("name").Find(&teams).Error
	return teams, err
}

// GetTeam returns the team with the given ID.
func GetTeam(id int64) (Team, error) {
	t := Team{}
	err := db.Where("id = ?", id).Take(&t).Error
	return t, err
}

// GetDefaultTeam returns the Default team, creating it if it doesn't exist.
func GetDefaultTeam() (Team, error) {
	t := Team{}
	err := db.Where("name = ?", DefaultTeamName).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t = Team{Name: DefaultTeamName, CreatedAt: time.Now().UTC()}
		err = db.Create(&t).Error
	}
	return t, err
}

// PostTeam creates a team.
func PostTeam(t *Team) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return ErrTeamName
	}
	if err := ensureTeamNameFree(t.Name, 0); err != nil {
		return err
	}
	t.Id = 0
	t.CreatedAt = time.Now().UTC()
	return db.Create(t).Error
}

// PutTeam renames a team.
func PutTeam(t *Team) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return ErrTeamName
	}
	if err := ensureTeamNameFree(t.Name, t.Id); err != nil {
		return err
	}
	return db.Model(&Team{}).Where("id = ?", t.Id).Update("name", t.Name).Error
}

// ensureTeamNameFree returns ErrTeamNameTaken if a team other than the one
// with the given ID has the name.
func ensureTeamNameFree(name string, id int64) error {
	var count int64
	err := db.Model(&Team{}).Where("name = ? AND id <> ?", name, id).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrTeamNameTaken
	}
	return nil
}

// TeamObjectResources are the API resources (first path segment) whose
// objects belong to a team, so that access to them depends on team
// membership and role.
var TeamObjectResources = map[string]bool{
	"campaigns": true,
	"groups":    true,
	"templates": true,
	"pages":     true,
	"smtp":      true,
}

// teamObjectModels are the objects that belong to a team.
var teamObjectModels = []interface{}{&Campaign{}, &Template{}, &Page{}, &Group{}, &SMTP{}}

// DeleteTeam deletes a team and its memberships. It refuses to delete a
// team that still has objects, or the only team.
func DeleteTeam(id int64) error {
	var teams int64
	if err := db.Model(&Team{}).Count(&teams).Error; err != nil {
		return err
	}
	if teams <= 1 {
		return ErrLastTeam
	}
	for _, m := range teamObjectModels {
		var count int64
		if err := db.Model(m).Where("team_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrTeamNotEmpty
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("team_id = ?", id).Delete(&TeamMember{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ?", id).Delete(&Team{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// GetTeamMembers returns the team's members, ordered by username.
func GetTeamMembers(tid int64) ([]TeamMemberDetail, error) {
	members := []TeamMemberDetail{}
	err := db.Table("team_members").
		Select("team_members.user_id, users.username, team_members.role").
		Joins("JOIN users ON users.id = team_members.user_id").
		Where("team_members.team_id = ?", tid).
		Order("users.username").
		Scan(&members).Error
	return members, err
}

// SetTeamMember adds the user to the team with the given role, or changes
// their role if they're already a member.
func SetTeamMember(tid, uid int64, role string) error {
	if !validTeamRole(role) {
		return ErrTeamRole
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("team_id = ? AND user_id = ?", tid, uid).Delete(&TeamMember{}).Error; err != nil {
			return err
		}
		return tx.Create(&TeamMember{TeamId: tid, UserId: uid, Role: role}).Error
	})
}

// RemoveTeamMember removes the user from the team.
func RemoveTeamMember(tid, uid int64) error {
	return db.Where("team_id = ? AND user_id = ?", tid, uid).Delete(&TeamMember{}).Error
}

// SetUserTeams replaces the user's memberships with the given ones.
func SetUserTeams(uid int64, memberships []TeamMember) error {
	for _, m := range memberships {
		if !validTeamRole(m.Role) {
			return ErrTeamRole
		}
		if _, err := GetTeam(m.TeamId); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", uid).Delete(&TeamMember{}).Error; err != nil {
			return err
		}
		for _, m := range memberships {
			m.UserId = uid
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetUserTeams returns the teams the user can use and their role on each.
// Users who can modify the system can edit every team.
func GetUserTeams(u User) ([]TeamMembership, error) {
	memberships := []TeamMembership{}
	admin, err := u.HasPermission(PermissionModifySystem)
	if err != nil {
		return memberships, err
	}
	if admin {
		teams, err := GetTeams()
		if err != nil {
			return memberships, err
		}
		for _, t := range teams {
			memberships = append(memberships, TeamMembership{Team: t, Role: TeamRoleEditor})
		}
		return memberships, nil
	}
	err = db.Table("teams").
		Select("teams.id, teams.name, teams.created_at, team_members.role").
		Joins("JOIN team_members ON team_members.team_id = teams.id").
		Where("team_members.user_id = ?", u.Id).
		Order("teams.name").
		Scan(&memberships).Error
	return memberships, err
}

// GetUserTeamRole returns the user's role on the team, or
// gorm.ErrRecordNotFound if they can't use it.
func GetUserTeamRole(u User, tid int64) (string, error) {
	teams, err := GetUserTeams(u)
	if err != nil {
		return "", err
	}
	for _, t := range teams {
		if t.Id == tid {
			return t.Role, nil
		}
	}
	return "", gorm.ErrRecordNotFound
}
