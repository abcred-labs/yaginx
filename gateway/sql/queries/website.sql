-- name: ListWebsites :many
SELECT id, name, specifications, hosts, proxy_rules, proxy_transforms, simple_responses FROM website ORDER BY id;
-- name: GetWebsiteByID :one
SELECT id, name, specifications, hosts, proxy_rules, proxy_transforms, simple_responses FROM website WHERE id = $1;
-- name: DeleteWebsite :execrows
DELETE FROM website WHERE id = $1;
