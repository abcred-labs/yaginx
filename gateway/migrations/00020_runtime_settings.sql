-- +goose Up
-- Runtime configuration is stored as one typed JSON document. The application
-- validates its shape against the embedded runtime schema before writing it.
ALTER TABLE gateway_system_setting
    ADD COLUMN IF NOT EXISTS updated_by_account_id bigint NULL;

CREATE INDEX IF NOT EXISTS ix_gateway_system_setting_updated_at
    ON gateway_system_setting (updated_at DESC);

-- +goose Down
SELECT 1;
