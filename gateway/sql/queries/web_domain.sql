-- name: ListWebDomains :many
SELECT id, name, is_use_free_cert, is_verified, coalesce(free_cert_message, '') AS free_cert_message FROM web_domain ORDER BY id;
-- name: GetWebDomainByID :one
SELECT id, name, is_use_free_cert, is_verified, coalesce(free_cert_message, '') AS free_cert_message FROM web_domain WHERE id = $1;
-- name: DeleteWebDomain :execrows
DELETE FROM web_domain WHERE id = $1;
