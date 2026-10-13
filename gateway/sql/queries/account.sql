-- name: GetAccountByEmail :one
SELECT id, email, password, password_hash FROM account WHERE lower(email) = lower($1) LIMIT 1;
-- name: GetProfile :one
SELECT id, email FROM account WHERE lower(email) = lower($1);
-- name: UpdateProfile :one
UPDATE account SET email = $1 WHERE lower(email) = lower($2) RETURNING id, email;
