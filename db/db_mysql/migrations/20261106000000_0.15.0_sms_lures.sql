-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE `smtp` ADD COLUMN sms_provider varchar(64) NOT NULL DEFAULT '';
ALTER TABLE `smtp` ADD COLUMN sms_from varchar(255) NOT NULL DEFAULT '';
ALTER TABLE `targets` ADD COLUMN phone varchar(64) NOT NULL DEFAULT '';
ALTER TABLE `results` ADD COLUMN phone varchar(64) NOT NULL DEFAULT '';
ALTER TABLE `email_requests` ADD COLUMN phone varchar(64) NOT NULL DEFAULT '';

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
ALTER TABLE `email_requests` DROP COLUMN phone;
ALTER TABLE `results` DROP COLUMN phone;
ALTER TABLE `targets` DROP COLUMN phone;
ALTER TABLE `smtp` DROP COLUMN sms_from;
ALTER TABLE `smtp` DROP COLUMN sms_provider;
