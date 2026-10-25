-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE `directory_sources` ADD COLUMN sync_interval bigint NOT NULL DEFAULT 0;

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
ALTER TABLE `directory_sources` DROP COLUMN sync_interval;
