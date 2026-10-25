
-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "tenants" (
    "id" integer primary key autoincrement,
    "name" varchar(255) NOT NULL UNIQUE,
    "created_at" datetime NOT NULL
);

-- Everything that existed before tenants belongs to one tenant.
INSERT INTO "tenants" ("name", "created_at") VALUES ('Default', CURRENT_TIMESTAMP);

-- Teams nest under a tenant. owner_user_id marks a user's private "personal"
-- team (0 means a normal shared team).
ALTER TABLE "teams" ADD COLUMN "tenant_id" bigint NOT NULL DEFAULT 0;
ALTER TABLE "teams" ADD COLUMN "owner_user_id" bigint NOT NULL DEFAULT 0;
UPDATE "teams" SET "tenant_id" = (SELECT "id" FROM "tenants" WHERE "name" = 'Default');
CREATE INDEX IF NOT EXISTS "teams_tenant_id" ON "teams" ("tenant_id");
CREATE INDEX IF NOT EXISTS "teams_owner_user_id" ON "teams" ("owner_user_id");

-- Each user belongs to one tenant and lands in a default team.
ALTER TABLE "users" ADD COLUMN "tenant_id" bigint NOT NULL DEFAULT 0;
ALTER TABLE "users" ADD COLUMN "default_team_id" bigint NOT NULL DEFAULT 0;
UPDATE "users" SET "tenant_id" = (SELECT "id" FROM "tenants" WHERE "name" = 'Default');
CREATE INDEX IF NOT EXISTS "users_tenant_id" ON "users" ("tenant_id");

-- Give every existing user a private personal team they own. Their default team
-- is left unset. The stored name just has to be unique; the UI shows it as
-- "Personal".
INSERT INTO "teams" ("name", "created_at", "tenant_id", "owner_user_id")
    SELECT 'Personal #' || "id", CURRENT_TIMESTAMP, "tenant_id", "id" FROM "users";
INSERT INTO "team_members" ("team_id", "user_id", "role")
    SELECT "id", "owner_user_id", 'editor' FROM "teams" WHERE "owner_user_id" <> 0;

-- The provider_admin role spans every tenant and can manage the tenants
-- themselves. It gets all object/system permissions plus manage_tenants.
INSERT INTO "permissions" ("slug", "name", "description")
    VALUES ('manage_tenants', 'Manage Tenants', 'Create and manage tenants across the system');
INSERT INTO "roles" ("slug", "name", "description")
    VALUES ('provider_admin', 'Provider Admin', 'Cross-tenant hosting operator with full permissions');
INSERT INTO "role_permissions" ("role_id", "permission_id")
    SELECT r.id, p.id FROM "roles" AS r, "permissions" AS p
    WHERE r.slug = 'provider_admin'
    AND p.slug IN ('view_objects', 'modify_objects', 'modify_system', 'manage_tenants');

-- Promote the bootstrap admin (the user named "admin", else the first user) to
-- provider_admin. Any other existing admins stay tenant admins of Default.
UPDATE "users" SET "role_id" = (SELECT "id" FROM "roles" WHERE "slug" = 'provider_admin')
WHERE "id" = (
    SELECT "id" FROM "users"
    WHERE "username" = 'admin' OR "id" = (SELECT MIN("id") FROM "users") LIMIT 1);

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DELETE FROM "team_members" WHERE "team_id" IN (SELECT "id" FROM "teams" WHERE "owner_user_id" <> 0);
DELETE FROM "teams" WHERE "owner_user_id" <> 0;
UPDATE "users" SET "role_id" = (SELECT "id" FROM "roles" WHERE "slug" = 'admin')
    WHERE "role_id" = (SELECT "id" FROM "roles" WHERE "slug" = 'provider_admin');
DELETE FROM "role_permissions" WHERE "role_id" = (SELECT "id" FROM "roles" WHERE "slug" = 'provider_admin');
DELETE FROM "roles" WHERE "slug" = 'provider_admin';
DELETE FROM "permissions" WHERE "slug" = 'manage_tenants';
DROP INDEX "users_tenant_id";
ALTER TABLE "users" DROP COLUMN "default_team_id";
ALTER TABLE "users" DROP COLUMN "tenant_id";
DROP INDEX "teams_owner_user_id";
DROP INDEX "teams_tenant_id";
ALTER TABLE "teams" DROP COLUMN "owner_user_id";
ALTER TABLE "teams" DROP COLUMN "tenant_id";
DROP TABLE "tenants";
