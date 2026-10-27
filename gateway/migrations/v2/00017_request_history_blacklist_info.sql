-- +goose Up
ALTER TABLE request_history
    ADD COLUMN blacklist_scope text,
    ADD COLUMN blacklist_rule_id bigint,
    ADD COLUMN blacklist_cidr cidr;

CREATE INDEX ix_request_history_blacklist_scope_ts
    ON request_history (blacklist_scope, request_ts DESC)
    WHERE blacklist_scope IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ix_request_history_blacklist_scope_ts;
ALTER TABLE request_history
    DROP COLUMN IF EXISTS blacklist_scope,
    DROP COLUMN IF EXISTS blacklist_rule_id,
    DROP COLUMN IF EXISTS blacklist_cidr;
