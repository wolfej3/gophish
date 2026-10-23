-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "teams" (
    "id" integer primary key autoincrement,
    "name" varchar(255) NOT NULL UNIQUE,
    "created_at" datetime NOT NULL
);
CREATE TABLE IF NOT EXISTS "team_members" (
    "team_id" bigint NOT NULL,
    "user_id" bigint NOT NULL,
    "role" varchar(16) NOT NULL,
    PRIMARY KEY ("team_id", "user_id")
);
CREATE INDEX IF NOT EXISTS "team_members_user_id" ON "team_members" ("user_id");

-- Everything that existed before teams is shared in one team, which every
-- existing user can edit.
INSERT INTO "teams" ("name", "created_at") VALUES ('Default', CURRENT_TIMESTAMP);
INSERT INTO "team_members" ("team_id", "user_id", "role")
    SELECT (SELECT "id" FROM "teams" WHERE "name" = 'Default'), "id", 'editor' FROM "users";

ALTER TABLE campaigns ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE groups ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE templates ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE pages ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE smtp ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE results ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE mail_logs ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE email_requests ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
UPDATE campaigns SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE groups SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE templates SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE pages SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE smtp SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE results SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE mail_logs SET team_id = (SELECT id FROM teams WHERE name = 'Default');
UPDATE email_requests SET team_id = (SELECT id FROM teams WHERE name = 'Default');
CREATE INDEX IF NOT EXISTS "campaigns_team_id" ON "campaigns" ("team_id");
CREATE INDEX IF NOT EXISTS "groups_team_id" ON "groups" ("team_id");
CREATE INDEX IF NOT EXISTS "templates_team_id" ON "templates" ("team_id");
CREATE INDEX IF NOT EXISTS "pages_team_id" ON "pages" ("team_id");
CREATE INDEX IF NOT EXISTS "smtp_team_id" ON "smtp" ("team_id");

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP INDEX "campaigns_team_id";
DROP INDEX "groups_team_id";
DROP INDEX "templates_team_id";
DROP INDEX "pages_team_id";
DROP INDEX "smtp_team_id";
ALTER TABLE campaigns DROP COLUMN team_id;
ALTER TABLE groups DROP COLUMN team_id;
ALTER TABLE templates DROP COLUMN team_id;
ALTER TABLE pages DROP COLUMN team_id;
ALTER TABLE smtp DROP COLUMN team_id;
ALTER TABLE results DROP COLUMN team_id;
ALTER TABLE mail_logs DROP COLUMN team_id;
ALTER TABLE email_requests DROP COLUMN team_id;
DROP TABLE "team_members";
DROP TABLE "teams";
