-- +goose Up
ALTER TABLE website
    ADD COLUMN IF NOT EXISTS data_schema_version integer NOT NULL DEFAULT 1;

ALTER TABLE website
    DROP CONSTRAINT IF EXISTS ck_website_data_schema_version;

ALTER TABLE website
    ADD CONSTRAINT ck_website_data_schema_version CHECK (data_schema_version > 0);

CREATE TABLE IF NOT EXISTS website_config_migration_error (
    website_id bigint PRIMARY KEY REFERENCES website(id) ON DELETE CASCADE,
    reason text NOT NULL,
    specifications jsonb,
    hosts jsonb,
    proxy_rules jsonb,
    proxy_transforms jsonb,
    simple_responses jsonb,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- +goose Down
DROP TABLE IF EXISTS website_config_migration_error;
ALTER TABLE website DROP CONSTRAINT IF EXISTS ck_website_data_schema_version;
ALTER TABLE website DROP COLUMN IF EXISTS data_schema_version;
