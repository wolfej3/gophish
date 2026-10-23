-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "audit_logs" (
    "id" integer primary key autoincrement,
    "created_at" datetime NOT NULL,
    "user_id" bigint,
    "username" varchar(255) NOT NULL DEFAULT '',
    "action" varchar(64) NOT NULL,
    "target" varchar(255) NOT NULL DEFAULT '',
    "details" text,
    "ip_address" varchar(64) NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS "audit_logs_username" ON "audit_logs" ("username");
CREATE INDEX IF NOT EXISTS "audit_logs_action" ON "audit_logs" ("action");

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE "audit_logs";
