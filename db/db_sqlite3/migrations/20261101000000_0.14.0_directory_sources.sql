-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "directory_sources" (
    "group_id" bigint NOT NULL,
    "connector" varchar(255) NOT NULL,
    "external_id" varchar(255) NOT NULL,
    "last_synced_at" datetime,
    "last_error" text,
    PRIMARY KEY ("group_id")
);

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE "directory_sources";
