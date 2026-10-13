-- +goose Up
-- v1-v5 stored encryption key versions as text while the then-active schema
-- numeric. Non-numeric historical values are unsafe because selecting a wrong
-- master key would make ciphertext ambiguous, so PostgreSQL aborts the cast.
ALTER TABLE gateway_system_setting
    ALTER COLUMN key_version TYPE integer USING key_version::integer;
ALTER TABLE gateway_certificate
    ALTER COLUMN key_version TYPE integer USING key_version::integer;
ALTER TABLE gateway_acme_account
    ALTER COLUMN key_version TYPE integer USING key_version::integer;

-- +goose Down
SELECT 1;
