-- +goose Up
-- Registration now creates active nodes and every active node must have a
-- number. Normalize rows created by the v2 baseline before tightening the
-- status and number constraints.
ALTER TABLE gateway_node DROP CONSTRAINT IF EXISTS ck_gateway_node_status;
ALTER TABLE gateway_node DROP CONSTRAINT IF EXISTS ck_gateway_node_approved_number;

WITH free_numbers AS (
    SELECT n::smallint AS node_number,
           row_number() OVER (ORDER BY n) AS ordinal
    FROM generate_series(0, 1023) n
    WHERE NOT EXISTS (SELECT 1 FROM gateway_node used WHERE used.node_number = n)
), numbered AS (
    SELECT node_id, free_numbers.node_number AS assigned_number
    FROM (
        SELECT node_id, row_number() OVER (ORDER BY created_at, node_id) AS ordinal
        FROM gateway_node
        WHERE status = 'pending'
    ) pending
    JOIN free_numbers USING (ordinal)
)
UPDATE gateway_node g
SET status = 'active',
    node_number = numbered.assigned_number,
    approved_at = COALESCE(g.approved_at, clock_timestamp()),
    state_changed_at = clock_timestamp()
FROM numbered
WHERE g.node_id = numbered.node_id;

UPDATE gateway_node
SET status = 'active',
    approved_at = COALESCE(approved_at, clock_timestamp()),
    state_changed_at = clock_timestamp()
WHERE status = 'approved';

ALTER TABLE gateway_node ALTER COLUMN status SET DEFAULT 'active';
ALTER TABLE gateway_node ADD CONSTRAINT ck_gateway_node_status
    CHECK (status IN ('active', 'draining', 'disabled'));
ALTER TABLE gateway_node ADD CONSTRAINT ck_gateway_node_active_number
    CHECK (node_number IS NOT NULL);

-- +goose Down
ALTER TABLE gateway_node DROP CONSTRAINT IF EXISTS ck_gateway_node_active_number;
ALTER TABLE gateway_node DROP CONSTRAINT IF EXISTS ck_gateway_node_status;
ALTER TABLE gateway_node ALTER COLUMN status SET DEFAULT 'pending';
ALTER TABLE gateway_node ADD CONSTRAINT ck_gateway_node_status
    CHECK (status IN ('pending', 'approved', 'draining', 'disabled'));
ALTER TABLE gateway_node ADD CONSTRAINT ck_gateway_node_approved_number CHECK (
    (status = 'pending' AND node_number IS NULL)
    OR (status IN ('approved', 'draining', 'disabled') AND node_number IS NOT NULL)
);
