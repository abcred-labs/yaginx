-- Run as the database owner after `yaginx database init`, then grant login roles
-- membership in yaginx_migrator or yaginx_gateway outside this file. This is
-- intentionally a deployment asset: normal migrations must not require
-- CREATEROLE on every production database connection.

DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'yaginx_migrator') THEN
        CREATE ROLE yaginx_migrator NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'yaginx_gateway') THEN
        CREATE ROLE yaginx_gateway NOLOGIN;
    END IF;
END
$roles$;

GRANT USAGE ON SCHEMA public TO yaginx_gateway;
GRANT SELECT, INSERT, UPDATE, DELETE ON
    account,
    web_domain,
    website,
    resource_monitor_info,
    resource_report,
    account_password_recovery,
    host_traffic,
    request_history,
    gateway_config_revision,
    gateway_config_event,
    gateway_node,
    gateway_acme_account,
    gateway_certificate,
    gateway_certificate_order,
    gateway_system_setting,
    gateway_api_credential,
    account_session,
    gateway_traffic_contribution,
    gateway_runtime_material,
    gateway_ip_blacklist,
    gateway_acme_key_value,
    yaginx_system_error,
    yaginx_system_error_metric
TO yaginx_gateway;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO yaginx_gateway;
GRANT SELECT ON yaginx_schema_version TO yaginx_gateway;
GRANT SELECT ON yaginx_schema_migration TO yaginx_gateway;
GRANT EXECUTE ON FUNCTION gateway_emit_config_event() TO yaginx_gateway;

GRANT USAGE, CREATE ON SCHEMA public TO yaginx_migrator;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO yaginx_migrator;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO yaginx_migrator;
GRANT EXECUTE ON FUNCTION gateway_emit_config_event() TO yaginx_migrator;
