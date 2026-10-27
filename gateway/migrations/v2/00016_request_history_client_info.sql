-- +goose Up
ALTER TABLE request_history
    ADD COLUMN user_agent_browser text,
    ADD COLUMN user_agent_browser_version text,
    ADD COLUMN user_agent_browser_major text,
    ADD COLUMN user_agent_os text,
    ADD COLUMN user_agent_os_version text,
    ADD COLUMN user_agent_os_major text,
    ADD COLUMN user_agent_device_type text,
    ADD COLUMN user_agent_device_brand text,
    ADD COLUMN user_agent_device_model text,
    ADD COLUMN user_agent_is_bot boolean,
    ADD COLUMN user_agent_bot_name text,
    ADD COLUMN user_agent_parse_status text;

CREATE INDEX ix_request_history_ua_browser_ts ON request_history (user_agent_browser, request_ts DESC);
CREATE INDEX ix_request_history_ua_os_ts ON request_history (user_agent_os, request_ts DESC);
CREATE INDEX ix_request_history_ua_device_type_ts ON request_history (user_agent_device_type, request_ts DESC);
CREATE INDEX ix_request_history_ua_bot_ts ON request_history (user_agent_is_bot, request_ts DESC);

-- +goose Down
DROP INDEX IF EXISTS ix_request_history_ua_bot_ts;
DROP INDEX IF EXISTS ix_request_history_ua_device_type_ts;
DROP INDEX IF EXISTS ix_request_history_ua_os_ts;
DROP INDEX IF EXISTS ix_request_history_ua_browser_ts;
ALTER TABLE request_history
    DROP COLUMN IF EXISTS user_agent_parse_status,
    DROP COLUMN IF EXISTS user_agent_bot_name,
    DROP COLUMN IF EXISTS user_agent_is_bot,
    DROP COLUMN IF EXISTS user_agent_device_model,
    DROP COLUMN IF EXISTS user_agent_device_brand,
    DROP COLUMN IF EXISTS user_agent_device_type,
    DROP COLUMN IF EXISTS user_agent_os_major,
    DROP COLUMN IF EXISTS user_agent_os_version,
    DROP COLUMN IF EXISTS user_agent_os,
    DROP COLUMN IF EXISTS user_agent_browser_major,
    DROP COLUMN IF EXISTS user_agent_browser_version,
    DROP COLUMN IF EXISTS user_agent_browser;
