-- name: ListHostTraffic :many
SELECT host_name, period, request_counts, inbound_bytes, outbound_bytes FROM host_traffic ORDER BY period DESC LIMIT $1;
