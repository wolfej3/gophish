-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS `sessions` (
    id bigint primary key auto_increment,
    token_hash varchar(64) NOT NULL UNIQUE,
    user_id bigint,
    data mediumblob,
    created_at datetime,
    expires_at datetime NOT NULL,
    INDEX sessions_user_id (user_id),
    INDEX sessions_expires_at (expires_at)
);

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE `sessions`;
