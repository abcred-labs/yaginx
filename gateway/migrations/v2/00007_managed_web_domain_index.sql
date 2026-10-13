-- +goose Up
-- Website host materialization uses ON CONFLICT (name). Keep the index in
-- the active v2 migration lineage so existing installations receive it.
CREATE UNIQUE INDEX IF NOT EXISTS ux_web_domain_name ON web_domain (name) WHERE name IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ux_web_domain_name;
