-- +goose Up
CREATE TABLE gateway_acme_key_value (
    key text PRIMARY KEY,
    value text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_gateway_acme_key_value_key CHECK (
        key ~ '^[A-Za-z0-9_-]+$' AND length(key) BETWEEN 1 AND 256
    )
);

CREATE INDEX ix_gateway_acme_key_value_expires_at
    ON gateway_acme_key_value (expires_at);

-- +goose Down
DROP TABLE gateway_acme_key_value;
