-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied.
-- No-op on SQLite: column lengths (varchar(255)/(512)) are declared but not
-- enforced, so the encrypted "enc:v1:" form fits the existing password
-- columns without a schema change. The paired MySQL migration widens them.
SELECT 1;

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back.
SELECT 1;
