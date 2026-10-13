-- +goose Up
-- Resource catalog activity aggregation reads the newest value per Host.
-- These indexes let PostgreSQL satisfy that aggregation from ordered index
-- scans instead of repeatedly sorting the full activity tables.
CREATE INDEX IF NOT EXISTS ix_host_traffic_catalog_host_period
    ON host_traffic (host_name, period DESC)
    WHERE host_name IS NOT NULL;

CREATE INDEX IF NOT EXISTS ix_request_history_catalog_host_ts
    ON request_history (host, request_ts DESC)
    WHERE host IS NOT NULL;

CREATE INDEX IF NOT EXISTS ix_resource_report_catalog_host_ts
    ON resource_report (resource_uuid, report_ts DESC);

-- +goose Down
DROP INDEX IF EXISTS ix_resource_report_catalog_host_ts;
DROP INDEX IF EXISTS ix_request_history_catalog_host_ts;
DROP INDEX IF EXISTS ix_host_traffic_catalog_host_period;
