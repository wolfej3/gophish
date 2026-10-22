-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "sessions" (
    "id" integer primary key autoincrement,
    "token_hash" varchar(64) NOT NULL UNIQUE,
    "user_id" bigint,
    "data" blob,
    "created_at" datetime,
    "expires_at" datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS "sessions_user_id" ON "sessions" ("user_id");
CREATE INDEX IF NOT EXISTS "sessions_expires_at" ON "sessions" ("expires_at");

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE "sessions";
