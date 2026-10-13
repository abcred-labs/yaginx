-- +goose Up
ALTER TABLE resource_report
    ADD COLUMN IF NOT EXISTS response_mode_metrics jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE gateway_traffic_contribution
    ADD COLUMN IF NOT EXISTS response_mode_metrics jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE gateway_traffic_contribution DROP COLUMN IF EXISTS response_mode_metrics;
ALTER TABLE resource_report DROP COLUMN IF EXISTS response_mode_metrics;
