-- +goose Up
-- Network topology and management access are node-local. Remove legacy
-- copies from the shared runtime document while preserving shared settings.
UPDATE gateway_system_setting
SET value = (value - 'listeners') || jsonb_build_object(
    'observability', COALESCE(value->'observability', '{}'::jsonb) - 'allowedCIDRs'
  ),
  updated_at = clock_timestamp()
WHERE setting_key = 'runtime'
  AND (value ? 'listeners' OR value #> '{observability,allowedCIDRs}' IS NOT NULL);

-- +goose Down
SELECT 1;
