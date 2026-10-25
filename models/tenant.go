package models

import (
	"errors"
	"strings"
	"time"

	log "github.com/gophish/gophish/logger"
	"gorm.io/gorm"
)

// DefaultTenantName is the tenant that holds everything created before tenants
// existed. New users and teams join it unless they're given another tenant.
const DefaultTenantName = "Default"

// ErrTenantName is returned when a tenant has no name.
var ErrTenantName = errors.New("Tenant name is required")

// ErrTenantNameTaken is returned when another tenant already has the name.
var ErrTenantNameTaken = errors.New("A tenant with that name already exists")

// ErrTenantNotEmpty is returned when deleting a tenant that still has users or
// shared teams.
var ErrTenantNotEmpty = errors.New("Move or delete the tenant's users and teams before deleting it")

// ErrLastTenant is returned when deleting the only tenant.
var ErrLastTenant = errors.New("Cannot delete the only tenant")

// Tenant is a client the system is hosted for. Teams nest under a tenant, and
// every user belongs to exactly one. Provider admins span all tenants.
type Tenant struct {
	Id        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// GetTenants returns every tenant, ordered by name.
func GetTenants() ([]Tenant, error) {
	tenants := []Tenant{}
	err := db.Order("name").Find(&tenants).Error
	return tenants, err
}

// GetTenant returns the tenant with the given ID.
func GetTenant(id int64) (Tenant, error) {
	t := Tenant{}
	err := db.Where("id = ?", id).Take(&t).Error
	return t, err
}

// GetDefaultTenant returns the Default tenant, creating it if it doesn't exist.
func GetDefaultTenant() (Tenant, error) {
	t := Tenant{}
	err := db.Where("name = ?", DefaultTenantName).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t = Tenant{Name: DefaultTenantName, CreatedAt: time.Now().UTC()}
		err = db.Create(&t).Error
	}
	return t, err
}

// PostTenant creates a tenant and seeds it with a shared default team so that
// users added to it have somewhere to land.
func PostTenant(t *Tenant) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return ErrTenantName
	}
	if err := ensureTenantNameFree(t.Name, 0); err != nil {
		return err
	}
	t.Id = 0
	t.CreatedAt = time.Now().UTC()
	if err := db.Create(t).Error; err != nil {
		return err
	}
	// Seed a shared default team. Team names are unique across tenants, so we
	// base it on the tenant's (unique) name. This is a convenience, so a name
	// clash isn't fatal to creating the tenant.
	team := Team{Name: t.Name + " Default", TenantId: t.Id, CreatedAt: time.Now().UTC()}
	if err := ensureTeamNameFree(team.Name, 0); err != nil {
		log.Infof("skipping default team for tenant %q: %v", t.Name, err)
		return nil
	}
	if err := db.Create(&team).Error; err != nil {
		log.Error(err)
	}
	return nil
}

// PutTenant renames a tenant.
func PutTenant(t *Tenant) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return ErrTenantName
	}
	if err := ensureTenantNameFree(t.Name, t.Id); err != nil {
		return err
	}
	return db.Model(&Tenant{}).Where("id = ?", t.Id).Update("name", t.Name).Error
}

// ensureTenantNameFree returns ErrTenantNameTaken if a tenant other than the
// one with the given ID has the name.
func ensureTenantNameFree(name string, id int64) error {
	var count int64
	err := db.Model(&Tenant{}).Where("name = ? AND id <> ?", name, id).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrTenantNameTaken
	}
	return nil
}

// DeleteTenant deletes a tenant. It refuses to delete the only tenant, or a
// tenant that still has users or shared teams.
func DeleteTenant(id int64) error {
	var tenants int64
	if err := db.Model(&Tenant{}).Count(&tenants).Error; err != nil {
		return err
	}
	if tenants <= 1 {
		return ErrLastTenant
	}
	var users int64
	if err := db.Model(&User{}).Where("tenant_id = ?", id).Count(&users).Error; err != nil {
		return err
	}
	if users > 0 {
		return ErrTenantNotEmpty
	}
	// Personal teams go with their users, so once there are no users only
	// shared teams can remain. Any of those must be moved or deleted first.
	var teams int64
	if err := db.Model(&Team{}).Where("tenant_id = ? AND owner_user_id = 0", id).Count(&teams).Error; err != nil {
		return err
	}
	if teams > 0 {
		return ErrTenantNotEmpty
	}
	result := db.Where("id = ?", id).Delete(&Tenant{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
