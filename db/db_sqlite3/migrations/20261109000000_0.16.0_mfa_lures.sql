-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE "pages" ADD COLUMN capture_mfa BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE "pages" ADD COLUMN mfa_prompt TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
ALTER TABLE "pages" DROP COLUMN mfa_prompt;
ALTER TABLE "pages" DROP COLUMN capture_mfa;
