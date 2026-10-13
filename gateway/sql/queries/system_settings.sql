-- name: GetSystemSetting :one
SELECT value FROM gateway_system_setting WHERE setting_key = $1;
-- name: UpsertSystemSetting :exec
INSERT INTO gateway_system_setting(setting_key, value, content_type, updated_at)
VALUES ($1,$2,$3,clock_timestamp())
ON CONFLICT(setting_key) DO UPDATE SET value=excluded.value, content_type=excluded.content_type, updated_at=excluded.updated_at;
