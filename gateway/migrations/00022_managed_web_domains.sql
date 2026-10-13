-- +goose Up
-- Domain rows are materialized by website Host configuration. Keep rows that
-- are no longer referenced so certificates and historical state can be reused
-- if a Host is added again, while the management query filters them out.
UPDATE web_domain
SET name = lower(trim(trailing '.' FROM name))
WHERE name IS NOT NULL;

WITH duplicates AS (
    SELECT id, row_number() OVER (PARTITION BY name ORDER BY id) AS ordinal
    FROM web_domain
    WHERE name IS NOT NULL
)
DELETE FROM web_domain d
USING duplicates x
WHERE d.id = x.id AND x.ordinal > 1;

CREATE UNIQUE INDEX IF NOT EXISTS ux_web_domain_name ON web_domain (name) WHERE name IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ux_web_domain_name;
