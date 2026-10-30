-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied.
-- Secrets (sending-profile and IMAP passwords) are encrypted at rest as
-- "enc:v1:" + base64(nonce||ciphertext), which is longer than the plaintext,
-- so widen these columns to hold the encrypted form. The Graph client_secret
-- column is already varchar(512).
ALTER TABLE smtp MODIFY password varchar(512);
ALTER TABLE imap MODIFY password varchar(512);

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back.
ALTER TABLE smtp MODIFY password varchar(255);
ALTER TABLE imap MODIFY password varchar(255);
