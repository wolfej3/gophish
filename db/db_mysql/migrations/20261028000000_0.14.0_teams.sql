-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS `teams` (
    id bigint primary key auto_increment,
    name varchar(255) NOT NULL UNIQUE,
    created_at datetime NOT NULL
);
CREATE TABLE IF NOT EXISTS `team_members` (
    team_id bigint NOT NULL,
    user_id bigint NOT NULL,
    role varchar(16) NOT NULL,
    PRIMARY KEY (team_id, user_id),
    INDEX team_members_user_id (user_id)
);

-- Everything that existed before teams is shared in one team, which every
-- existing user can edit.
INSERT INTO `teams` (name, created_at) VALUES ('Default', UTC_TIMESTAMP());
INSERT INTO `team_members` (team_id, user_id, role)
    SELECT (SELECT id FROM `teams` WHERE name = 'Default'), id, 'editor' FROM `users`;

ALTER TABLE `campaigns` ADD COLUMN team_id bigint NOT NULL DEFAULT 0, ADD INDEX campaigns_team_id (team_id);
ALTER TABLE `groups` ADD COLUMN team_id bigint NOT NULL DEFAULT 0, ADD INDEX groups_team_id (team_id);
ALTER TABLE `templates` ADD COLUMN team_id bigint NOT NULL DEFAULT 0, ADD INDEX templates_team_id (team_id);
ALTER TABLE `pages` ADD COLUMN team_id bigint NOT NULL DEFAULT 0, ADD INDEX pages_team_id (team_id);
ALTER TABLE `smtp` ADD COLUMN team_id bigint NOT NULL DEFAULT 0, ADD INDEX smtp_team_id (team_id);
ALTER TABLE `results` ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE `mail_logs` ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
ALTER TABLE `email_requests` ADD COLUMN team_id bigint NOT NULL DEFAULT 0;
UPDATE `campaigns` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `groups` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `templates` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `pages` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `smtp` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `results` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `mail_logs` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');
UPDATE `email_requests` SET team_id = (SELECT id FROM `teams` WHERE name = 'Default');

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
ALTER TABLE `campaigns` DROP INDEX campaigns_team_id, DROP COLUMN team_id;
ALTER TABLE `groups` DROP INDEX groups_team_id, DROP COLUMN team_id;
ALTER TABLE `templates` DROP INDEX templates_team_id, DROP COLUMN team_id;
ALTER TABLE `pages` DROP INDEX pages_team_id, DROP COLUMN team_id;
ALTER TABLE `smtp` DROP INDEX smtp_team_id, DROP COLUMN team_id;
ALTER TABLE `results` DROP COLUMN team_id;
ALTER TABLE `mail_logs` DROP COLUMN team_id;
ALTER TABLE `email_requests` DROP COLUMN team_id;
DROP TABLE `team_members`;
DROP TABLE `teams`;
