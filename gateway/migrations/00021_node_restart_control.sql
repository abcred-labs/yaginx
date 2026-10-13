-- +goose Up
ALTER TABLE gateway_node
    ADD COLUMN IF NOT EXISTS restart_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS observed_restart_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS restart_requested_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS restart_completed_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS restart_error text NOT NULL DEFAULT '';

ALTER TABLE gateway_node
    DROP CONSTRAINT IF EXISTS ck_gateway_node_restart_generation_nonnegative;

ALTER TABLE gateway_node
    ADD CONSTRAINT ck_gateway_node_restart_generation_nonnegative
    CHECK (restart_generation >= 0 AND observed_restart_generation >= 0);

-- +goose Down
SELECT 1;
