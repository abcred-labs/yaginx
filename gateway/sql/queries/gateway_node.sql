-- name: ListGatewayNodes :many
SELECT node_id, status, node_number, applied_revision, last_heartbeat_at FROM gateway_node ORDER BY created_at;
-- name: HeartbeatGatewayNode :one
UPDATE gateway_node SET applied_revision=$2, last_heartbeat_at=clock_timestamp() WHERE node_id=$1 RETURNING status, node_number;
