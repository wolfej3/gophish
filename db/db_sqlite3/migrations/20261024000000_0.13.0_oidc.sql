-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE users ADD COLUMN oidc_identity varchar(512) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS "users_oidc_identity" ON "users" ("oidc_identity");

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP INDEX "users_oidc_identity";
ALTER TABLE users DROP COLUMN oidc_identity;
