-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE `users` ADD COLUMN saml_identity varchar(512) NOT NULL DEFAULT '';
CREATE INDEX users_saml_identity ON `users` (saml_identity);

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP INDEX users_saml_identity ON `users`;
ALTER TABLE `users` DROP COLUMN saml_identity;
