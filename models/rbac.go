package models

/*
Design:

Gophish implements simple Role-Based-Access-Control (RBAC) to control access to
certain resources.

Each user is assigned a single global role:

* Provider Admin - The hosting operator. Spans every tenant, manages the
                   tenants themselves, and can act on all teams and objects.
* Admin          - A tenant administrator. Can modify all objects and
                   system-level configuration within their own tenant.
* User           - Can modify objects in the teams they belong to.

Objects belong to teams, and teams nest under tenants (see team.go and
tenant.go). A user's global role combines with their tenant and team
memberships to decide what they can reach: GetUserTeams is where the role,
tenant, and membership rules come together.

Each role maps to one or more permissions, making it easy to add more granular
permissions over time.

This is supported through a simple API on a user object,
`HasPermission(Permission)`, which returns a boolean and an error.
This API checks the role associated with the user to see if that role has the
requested permission.
*/

const (
	// RoleProviderAdmin is used for the hosting operator. Users with this role
	// span every tenant: they manage the tenants themselves and have full
	// access to all teams and objects across the system.
	RoleProviderAdmin = "provider_admin"
	// RoleAdmin is used for Gophish tenant administrators. Users with this role
	// can manage all objects and system-level configuration within their own
	// tenant, such as its users and teams.
	RoleAdmin = "admin"
	// RoleUser is used for standard Gophish users. Users with this role can
	// create, manage, and view Gophish objects and campaigns.
	RoleUser = "user"

	// PermissionViewObjects determines if a role can view standard Gophish
	// objects such as campaigns, groups, landing pages, etc.
	PermissionViewObjects = "view_objects"
	// PermissionModifyObjects determines if a role can create and modify
	// standard Gophish objects.
	PermissionModifyObjects = "modify_objects"
	// PermissionModifySystem determines if a role can manage system-level
	// configuration.
	PermissionModifySystem = "modify_system"
	// PermissionManageTenants determines if a role can create and manage
	// tenants, and act across every tenant. Only the provider_admin role has
	// it.
	PermissionManageTenants = "manage_tenants"
)

// Role represents a user role within Gophish. Each user has a single role
// which maps to a set of permissions.
type Role struct {
	ID          int64        `json:"-"`
	Slug        string       `json:"slug"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"-" gorm:"many2many:role_permissions;"`
}

// Permission determines what a particular role can do. Each role may have one
// or more permissions.
type Permission struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetRoleBySlug returns a role that can be assigned to a user.
func GetRoleBySlug(slug string) (Role, error) {
	role := Role{}
	err := db.Where("slug=?", slug).First(&role).Error
	return role, err
}

// HasPermission checks to see if the user has a role with the requested
// permission.
func (u *User) HasPermission(slug string) (bool, error) {
	perm := []Permission{}
	err := db.Model(&Role{ID: u.RoleID}).Where("slug=?", slug).Association("Permissions").Find(&perm)
	if err != nil {
		return false, err
	}
	// Gorm doesn't return an ErrRecordNotFound whe scanning into a slice, so
	// we need to check the length (ref jinzhu/gorm#228)
	if len(perm) == 0 {
		return false, nil
	}
	return true, nil
}
