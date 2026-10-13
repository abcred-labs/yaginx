-- +goose Up
ALTER TABLE gateway_system_setting
    ADD COLUMN IF NOT EXISTS content_type text NOT NULL DEFAULT 'application/json';

-- +goose Down
SELECT 1;
