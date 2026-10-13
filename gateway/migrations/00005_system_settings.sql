-- +goose Up
CREATE TABLE IF NOT EXISTS gateway_system_setting (
    setting_key text PRIMARY KEY,
    encrypted_value bytea NOT NULL,
    nonce bytea NOT NULL,
    key_version text NOT NULL,
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS tr_yaginx_system_setting_config_event ON gateway_system_setting;
CREATE TRIGGER tr_yaginx_system_setting_config_event
AFTER INSERT OR UPDATE OR DELETE ON gateway_system_setting
FOR EACH ROW EXECUTE FUNCTION yaginx_publish_config_event();

-- +goose Down
SELECT 1;
