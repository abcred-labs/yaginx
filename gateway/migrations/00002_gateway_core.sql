-- +goose Up
CREATE TABLE IF NOT EXISTS gateway_node (
    node_id text PRIMARY KEY,
    fingerprint text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    node_number smallint NULL UNIQUE,
    address text NULL,
    version text NULL,
    applied_revision bigint NOT NULL DEFAULT 0,
    started_at timestamp with time zone NULL,
    last_heartbeat_at timestamp with time zone NOT NULL DEFAULT now(),
    last_error text NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT ck_gateway_node_status CHECK (status IN ('pending', 'approved', 'disabled')),
    CONSTRAINT ck_gateway_node_number CHECK (node_number IS NULL OR node_number BETWEEN 0 AND 1023)
);

CREATE INDEX IF NOT EXISTS ix_gateway_node_status_heartbeat
    ON gateway_node (status, last_heartbeat_at DESC);

-- +goose Down
SELECT 1;
