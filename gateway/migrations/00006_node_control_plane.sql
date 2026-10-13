-- +goose Up
-- The earlier prototype used a text node identifier. A non-UUID historic node
-- is unsafe to auto-repair because it could silently become a different node;
-- PostgreSQL therefore aborts this migration instead of inventing an ID.
ALTER TABLE gateway_certificate_order
    DROP CONSTRAINT IF EXISTS gateway_certificate_order_node_id_fkey;
ALTER TABLE gateway_node
    ALTER COLUMN node_id TYPE uuid USING node_id::uuid,
    ADD COLUMN IF NOT EXISTS machine_fingerprint char(64) NOT NULL DEFAULT repeat('0', 64),
    ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS software_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS git_sha text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reload_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS observed_reload_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_snapshot_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS last_snapshot_error text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS state_reason text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS state_changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ADD COLUMN IF NOT EXISTS approved_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS approved_by_account_id bigint NULL,
    ADD COLUMN IF NOT EXISTS drain_started_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS disabled_at timestamptz NULL;
ALTER TABLE gateway_certificate_order
    ALTER COLUMN node_id TYPE uuid USING node_id::uuid;
ALTER TABLE gateway_node DROP CONSTRAINT IF EXISTS ck_gateway_node_status;
ALTER TABLE gateway_node ADD CONSTRAINT ck_gateway_node_status
    CHECK (status IN ('pending', 'approved', 'draining', 'disabled'));
ALTER TABLE gateway_node DROP CONSTRAINT IF EXISTS ck_gateway_node_approved_number;
ALTER TABLE gateway_node ADD CONSTRAINT ck_gateway_node_approved_number CHECK (
    (status = 'pending' AND node_number IS NULL)
    OR (status IN ('approved', 'draining', 'disabled') AND node_number IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_gateway_node_node_number
    ON gateway_node (node_number) WHERE node_number IS NOT NULL;
ALTER TABLE gateway_certificate_order
    ADD CONSTRAINT gateway_certificate_order_node_id_fkey
    FOREIGN KEY (node_id) REFERENCES gateway_node(node_id) ON DELETE RESTRICT;

-- +goose Down
SELECT 1;
