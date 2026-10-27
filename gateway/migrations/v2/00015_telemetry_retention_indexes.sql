-- +goose NO TRANSACTION
-- +goose Up
-- Retention and analytics fallback repeatedly look up all process rows for a
-- host/cycle/bucket. The existing period-leading index is optimized for
-- range scans, while this index keeps the completeness guard bounded.
CREATE INDEX CONCURRENTLY IF NOT EXISTS ix_gateway_traffic_contribution_host_cycle_period
    ON gateway_traffic_contribution (host_name, cycle_type, period);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS ix_gateway_traffic_contribution_host_cycle_period;
