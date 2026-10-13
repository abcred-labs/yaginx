-- +goose Up
-- v2 is the explicit break from the legacy management contract. Persisted
-- website JSON is normalized once here; runtime code intentionally has no
-- PascalCase or pre-v2 fallback path.
UPDATE website
SET specifications = jsonb_strip_nulls(
    (COALESCE(specifications, '{}'::jsonb)
        - 'DefaultHost'
        - 'DefaultDestination'
        - 'DefaultDestinationHost'
        - 'WebProxy'
        - 'AuthorizationPolicy'
        - 'Hosts'
        - 'ProxyRules'
        - 'ProxyTransforms'
        - 'SimpleResponses'
        - 'IsWithOriginalHostHeader'
        - 'IsAllowUnsafeSslCertificate'
        - 'IsAutoRedirectHttp2Https'
        - 'isWithOriginalHostHeader'
        - 'isAllowUnsafeSslCertificate'
        - 'isAutoRedirectHttp2Https')
    || jsonb_build_object(
        'defaultHost', COALESCE(specifications->'defaultHost', specifications->'DefaultHost'),
        'defaultDestination', COALESCE(specifications->'defaultDestination', specifications->'DefaultDestination'),
        'defaultDestinationHost', COALESCE(specifications->'defaultDestinationHost', specifications->'DefaultDestinationHost'),
        'webProxy', COALESCE(specifications->'webProxy', specifications->'WebProxy'),
        'authorizationPolicy', COALESCE(specifications->'authorizationPolicy', specifications->'AuthorizationPolicy'),
        'hosts', COALESCE(specifications->'hosts', specifications->'Hosts'),
        'proxyRules', COALESCE(specifications->'proxyRules', specifications->'ProxyRules'),
        'proxyTransforms', COALESCE(specifications->'proxyTransforms', specifications->'ProxyTransforms'),
        'simpleResponses', COALESCE(specifications->'simpleResponses', specifications->'SimpleResponses'),
        'preserveOriginalHost', COALESCE(specifications->'preserveOriginalHost', specifications->'isWithOriginalHostHeader', specifications->'IsWithOriginalHostHeader'),
        'allowInvalidTlsCertificate', COALESCE(specifications->'allowInvalidTlsCertificate', specifications->'isAllowUnsafeSslCertificate', specifications->'IsAllowUnsafeSslCertificate'),
        'redirectHttpToHttps', COALESCE(specifications->'redirectHttpToHttps', specifications->'isAutoRedirectHttp2Https', specifications->'IsAutoRedirectHttp2Https')
    )
)
WHERE specifications IS NOT NULL;

UPDATE website
SET proxy_rules = COALESCE((
    SELECT jsonb_agg(jsonb_strip_nulls(
        (rule
            - 'Name' - 'Description' - 'IsEnabled' - 'PathPattern'
            - 'IsRegexMatchPattern' - 'Destination' - 'DestinationHost'
            - 'WebProxy' - 'RemovePrefix'
            - 'IsWithOriginalHostHeader' - 'IsAllowUnsafeSslCertificate'
            - 'isWithOriginalHostHeader' - 'isAllowUnsafeSslCertificate')
        || jsonb_build_object(
            'name', COALESCE(rule->'name', rule->'Name'),
            'description', COALESCE(rule->'description', rule->'Description'),
            'isEnabled', COALESCE(rule->'isEnabled', rule->'IsEnabled'),
            'pathPattern', COALESCE(rule->'pathPattern', rule->'PathPattern'),
            'isRegexMatchPattern', COALESCE(rule->'isRegexMatchPattern', rule->'IsRegexMatchPattern'),
            'destination', COALESCE(rule->'destination', rule->'Destination'),
            'destinationHost', COALESCE(rule->'destinationHost', rule->'DestinationHost'),
            'webProxy', COALESCE(rule->'webProxy', rule->'WebProxy'),
            'removePrefix', COALESCE(rule->'removePrefix', rule->'RemovePrefix'),
            'preserveOriginalHost', COALESCE(rule->'preserveOriginalHost', rule->'isWithOriginalHostHeader', rule->'IsWithOriginalHostHeader'),
            'allowInvalidTlsCertificate', COALESCE(rule->'allowInvalidTlsCertificate', rule->'isAllowUnsafeSslCertificate', rule->'IsAllowUnsafeSslCertificate')
        )
    ))
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(proxy_rules) = 'array' THEN proxy_rules ELSE '[]'::jsonb END
    ) AS rule
), '[]'::jsonb)
WHERE proxy_rules IS NOT NULL;

UPDATE website
SET simple_responses = COALESCE((
    SELECT jsonb_agg(jsonb_strip_nulls(
        (response
            - 'Url' - 'StatusCode' - 'ContentType' - 'ResponseContent'
            - 'ExtraHeaders' - 'url')
        || jsonb_build_object(
            'path', COALESCE(response->'path', response->'url', response->'Url'),
            'statusCode', COALESCE(response->'statusCode', response->'StatusCode'),
            'contentType', COALESCE(response->'contentType', response->'ContentType'),
            'responseContent', COALESCE(response->'responseContent', response->'ResponseContent'),
            'extraHeaders', COALESCE(response->'extraHeaders', response->'ExtraHeaders')
        )
    ))
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(simple_responses) = 'array' THEN simple_responses ELSE '[]'::jsonb END
    ) AS response
), '[]'::jsonb)
WHERE simple_responses IS NOT NULL;

UPDATE gateway_certificate
SET source = 'imported_pfx'
WHERE source = 'legacy_pfx';

ALTER TABLE gateway_certificate
    DROP CONSTRAINT IF EXISTS ck_gateway_certificate_source;
ALTER TABLE gateway_certificate
    ADD CONSTRAINT ck_gateway_certificate_source
    CHECK (source IN ('imported_pfx', 'acme')) NOT VALID;

-- The bridge reaches the same final configuration-event contract as a clean
-- v2 initialization. Historical trigger output used uppercase operations and
-- the table name as resource type; v2 stores explicit lower-case values.
UPDATE gateway_config_event
SET operation = lower(operation)
WHERE operation <> lower(operation);

ALTER TABLE gateway_config_event
    DROP CONSTRAINT IF EXISTS ck_gateway_config_event_operation,
    DROP CONSTRAINT IF EXISTS ck_gateway_config_event_resource_type;
ALTER TABLE gateway_config_event
    ADD CONSTRAINT ck_gateway_config_event_operation
        CHECK (operation IN ('insert', 'update', 'delete')),
    ADD CONSTRAINT ck_gateway_config_event_resource_type
        CHECK (resource_type IN (
            'website',
            'web_domain',
            'gateway_certificate',
            'gateway_system_setting',
            'gateway_runtime_material',
            'gateway_ip_blacklist'
        ));

DROP TRIGGER IF EXISTS tr_yaginx_ip_blacklist_config_event ON gateway_ip_blacklist;
DROP TRIGGER IF EXISTS trg_gateway_ip_blacklist_config_event ON gateway_ip_blacklist;
CREATE TRIGGER trg_gateway_ip_blacklist_config_event
AFTER INSERT OR UPDATE OR DELETE ON gateway_ip_blacklist
FOR EACH ROW EXECUTE FUNCTION gateway_emit_config_event('id', 'gateway_ip_blacklist');
DROP FUNCTION IF EXISTS yaginx_publish_config_event();

ALTER TABLE gateway_config_revision
    DROP CONSTRAINT IF EXISTS gateway_config_revision_pkey;
DROP INDEX IF EXISTS ux_gateway_config_revision_singleton_id;
ALTER TABLE gateway_config_revision
    DROP COLUMN IF EXISTS singleton,
    ADD CONSTRAINT gateway_config_revision_pkey PRIMARY KEY (singleton_id);

CREATE TABLE IF NOT EXISTS yaginx_schema_version (
    generation smallint PRIMARY KEY CHECK (generation = 2),
    initialized_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO yaginx_schema_version (generation) VALUES (2)
ON CONFLICT (generation) DO NOTHING;

-- +goose Down
SELECT 1;
