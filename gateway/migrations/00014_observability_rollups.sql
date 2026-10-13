-- +goose Up
-- Preserve the legacy report shape while adding the dimensions required by
-- the single-node operations dashboard. JSON maps are bounded by gateway
-- controlled keys; user supplied Hosts, paths and IPs are never keys here.
ALTER TABLE gateway_traffic_contribution
    ADD COLUMN IF NOT EXISTS error_counts jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS latency_histogram jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE gateway_traffic_contribution
    ADD COLUMN IF NOT EXISTS response_mode_metrics jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE resource_report
    ADD COLUMN IF NOT EXISTS inbound_bytes bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS outbound_bytes bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS error_counts jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS latency_histogram jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS response_mode_metrics jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS is_partial boolean NOT NULL DEFAULT false;

ALTER TABLE request_history
    ADD COLUMN IF NOT EXISTS trace_id text NULL;

CREATE INDEX IF NOT EXISTS ix_resource_report_cycle_period
    ON resource_report (cycle_type, report_ts DESC);

CREATE INDEX IF NOT EXISTS ix_request_history_host_ts
    ON request_history (host, request_ts DESC);

-- +goose Down
SELECT 1;
