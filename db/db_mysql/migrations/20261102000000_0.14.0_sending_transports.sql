-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE smtp
    ADD COLUMN tenant_id varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN client_id varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN client_secret varchar(512) NOT NULL DEFAULT '',
    ADD COLUMN credentials_file varchar(1024) NOT NULL DEFAULT '';

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
ALTER TABLE smtp
    DROP COLUMN credentials_file,
    DROP COLUMN client_secret,
    DROP COLUMN client_id,
    DROP COLUMN tenant_id;
