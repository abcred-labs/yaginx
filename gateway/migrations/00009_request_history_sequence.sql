-- +goose Up
-- The legacy request_history table uses caller-supplied bigint identifiers.
-- Runtime writers need a collision-safe source that also remains above every
-- imported legacy identifier, so the sequence is initialized once under the
-- fixed migration advisory lock.
CREATE SEQUENCE IF NOT EXISTS gateway_request_history_id_seq AS bigint;
SELECT setval(
    'gateway_request_history_id_seq',
    GREATEST(COALESCE((SELECT max(id) FROM request_history), 0), 1000000000000000),
    true
);

-- +goose Down
SELECT 1;
