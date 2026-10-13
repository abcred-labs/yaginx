-- +goose Up
-- report_ts is the canonical report bucket. Repair historical timestamptz
-- mirrors written as PostgreSQL infinity or another inconsistent timestamp.
UPDATE resource_report
SET report_time = to_timestamp(report_ts),
    update_time = clock_timestamp()
WHERE report_time IS DISTINCT FROM to_timestamp(report_ts);

-- +goose Down
SELECT 1;
