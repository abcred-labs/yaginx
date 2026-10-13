-- +goose Up
-- Keep the oldest operator-created website when prior UI saves inserted the
-- same name more than once. Deletes emit normal configuration events.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY name
               ORDER BY create_time ASC, id ASC
           ) AS row_number
    FROM website
    WHERE name IS NOT NULL
)
DELETE FROM website AS duplicate
USING ranked
WHERE duplicate.id = ranked.id
  AND ranked.row_number > 1;

CREATE UNIQUE INDEX IF NOT EXISTS ux_website_name
    ON website (name)
    WHERE name IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ux_website_name;
