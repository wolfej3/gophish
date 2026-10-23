-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "api_keys" (
    "id" integer primary key autoincrement,
    "user_id" bigint NOT NULL,
    "name" varchar(255) NOT NULL,
    "prefix" varchar(32) NOT NULL,
    "key_hash" varchar(64) NOT NULL UNIQUE,
    "access" varchar(16) NOT NULL,
    "resources" varchar(255) NOT NULL,
    "created_at" datetime NOT NULL,
    "last_used_at" datetime,
    "expires_at" datetime
);
CREATE INDEX IF NOT EXISTS "api_keys_user_id" ON "api_keys" ("user_id");

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE "api_keys";
