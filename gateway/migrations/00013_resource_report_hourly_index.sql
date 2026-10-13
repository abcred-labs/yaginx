-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS ix_resource_report_hourly_report_ts
    ON resource_report (report_ts, resource_uuid)
    WHERE cycle_type = 10;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS ix_resource_report_hourly_report_ts;
