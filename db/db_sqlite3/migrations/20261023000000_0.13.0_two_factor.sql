-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE users ADD COLUMN totp_secret varchar(255) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled boolean NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_last_step bigint NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS "recovery_codes" (
    "id" integer primary key autoincrement,
    "user_id" bigint NOT NULL,
    "code_hash" varchar(64) NOT NULL,
    "used_at" datetime
);
CREATE INDEX IF NOT EXISTS "recovery_codes_user_id" ON "recovery_codes" ("user_id");

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE "recovery_codes";
ALTER TABLE users DROP COLUMN totp_last_step;
ALTER TABLE users DROP COLUMN totp_enabled;
ALTER TABLE users DROP COLUMN totp_secret;
