-- name: ListRequestHistory :many
SELECT id, request_time, method, host, path, status_code, duration_ms FROM request_history ORDER BY request_ts DESC LIMIT $1;
-- name: GetRequestHistory :one
SELECT request_time, method, host, path, query_string, status_code, duration_ms FROM request_history WHERE id = $1;
