-- +goose Up
-- The Go gateway now stores shared runtime material and operational data in
-- PostgreSQL plaintext. Do not silently discard ciphertext created by older
-- releases: an operator must back it up and rebuild it explicitly.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM gateway_certificate)
       OR EXISTS (SELECT 1 FROM gateway_acme_account)
       OR EXISTS (SELECT 1 FROM gateway_system_setting) THEN
        RAISE EXCEPTION 'cannot migrate encrypted gateway data to plaintext: back up and clear gateway_certificate, gateway_acme_account, and gateway_system_setting first';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE gateway_certificate
    DROP CONSTRAINT IF EXISTS ck_gateway_certificate_nonce,
    DROP CONSTRAINT IF EXISTS ck_gateway_certificate_key_version,
    DROP COLUMN IF EXISTS encrypted_payload,
    DROP COLUMN IF EXISTS nonce,
    DROP COLUMN IF EXISTS key_version,
    ADD COLUMN certificate_pem bytea NOT NULL,
    ADD COLUMN private_key_pem bytea NOT NULL;

ALTER TABLE gateway_acme_account
    DROP CONSTRAINT IF EXISTS ck_gateway_acme_account_nonce,
    DROP CONSTRAINT IF EXISTS ck_gateway_acme_account_key_version,
    DROP COLUMN IF EXISTS encrypted_key,
    DROP COLUMN IF EXISTS nonce,
    DROP COLUMN IF EXISTS key_version,
    ADD COLUMN private_key_pem bytea NOT NULL,
    ADD COLUMN registration_json bytea NOT NULL DEFAULT ''::bytea;

ALTER TABLE gateway_system_setting
    DROP CONSTRAINT IF EXISTS ck_gateway_system_setting_nonce,
    DROP CONSTRAINT IF EXISTS ck_gateway_system_setting_key_version,
    DROP COLUMN IF EXISTS encrypted_value,
    DROP COLUMN IF EXISTS nonce,
    DROP COLUMN IF EXISTS key_version,
    ADD COLUMN value jsonb NOT NULL;

CREATE TABLE gateway_runtime_material (
    singleton_id smallint PRIMARY KEY DEFAULT 1 CHECK (singleton_id = 1),
    jwt_current text NOT NULL CHECK (length(jwt_current) >= 32),
    jwt_previous text NULL CHECK (jwt_previous IS NULL OR length(jwt_previous) >= 32),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE gateway_config_event
    DROP CONSTRAINT IF EXISTS ck_gateway_config_event_resource_type,
    ADD CONSTRAINT ck_gateway_config_event_resource_type
    CHECK (resource_type IN ('website', 'web_domain', 'gateway_certificate', 'gateway_system_setting', 'gateway_runtime_material')) NOT VALID;

DROP TRIGGER IF EXISTS trg_gateway_runtime_material_config_event ON gateway_runtime_material;
CREATE TRIGGER trg_gateway_runtime_material_config_event
AFTER INSERT OR UPDATE OR DELETE ON gateway_runtime_material
FOR EACH ROW EXECUTE FUNCTION gateway_emit_config_event('singleton_id', 'gateway_runtime_material');

-- +goose Down
SELECT 1;
