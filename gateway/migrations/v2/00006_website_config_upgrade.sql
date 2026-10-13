-- +goose Up
-- Normalize every website collection before declaring the row v2. Rows that
-- cannot be converted remain v1 and are recorded for operator review.
WITH normalized AS (
    SELECT w.id,
           CASE WHEN jsonb_typeof(COALESCE(w.specifications,'{}'::jsonb))='object' THEN (COALESCE(w.specifications,'{}'::jsonb) - ARRAY['DefaultHost','DefaultDestination','DefaultDestinationHost','WebProxy','AuthorizationPolicy','IsWithOriginalHostHeader','IsAllowUnsafeSslCertificate','IsAutoRedirectHttp2Https','IsMaintenanceMode','IsAllowAnySourceCors']
            || CASE WHEN w.specifications ? 'defaultHost' OR NOT w.specifications ? 'DefaultHost' THEN '{}'::jsonb ELSE jsonb_build_object('defaultHost',w.specifications->'DefaultHost') END
            || CASE WHEN w.specifications ? 'defaultDestination' OR NOT w.specifications ? 'DefaultDestination' THEN '{}'::jsonb ELSE jsonb_build_object('defaultDestination',w.specifications->'DefaultDestination') END
            || CASE WHEN w.specifications ? 'defaultDestinationHost' OR NOT w.specifications ? 'DefaultDestinationHost' THEN '{}'::jsonb ELSE jsonb_build_object('defaultDestinationHost',w.specifications->'DefaultDestinationHost') END
            || CASE WHEN w.specifications ? 'webProxy' OR NOT w.specifications ? 'WebProxy' THEN '{}'::jsonb ELSE jsonb_build_object('webProxy',w.specifications->'WebProxy') END
            || CASE WHEN w.specifications ? 'authorizationPolicy' OR NOT w.specifications ? 'AuthorizationPolicy' THEN '{}'::jsonb ELSE jsonb_build_object('authorizationPolicy',w.specifications->'AuthorizationPolicy') END
            || CASE WHEN w.specifications ? 'preserveOriginalHost' OR NOT w.specifications ? 'IsWithOriginalHostHeader' THEN '{}'::jsonb ELSE jsonb_build_object('preserveOriginalHost',w.specifications->'IsWithOriginalHostHeader') END
            || CASE WHEN w.specifications ? 'allowInvalidTlsCertificate' OR NOT w.specifications ? 'IsAllowUnsafeSslCertificate' THEN '{}'::jsonb ELSE jsonb_build_object('allowInvalidTlsCertificate',w.specifications->'IsAllowUnsafeSslCertificate') END
            || CASE WHEN w.specifications ? 'redirectHttpToHttps' OR NOT w.specifications ? 'IsAutoRedirectHttp2Https' THEN '{}'::jsonb ELSE jsonb_build_object('redirectHttpToHttps',w.specifications->'IsAutoRedirectHttp2Https') END
            || CASE WHEN w.specifications ? 'isMaintenanceMode' OR NOT w.specifications ? 'IsMaintenanceMode' THEN '{}'::jsonb ELSE jsonb_build_object('isMaintenanceMode',w.specifications->'IsMaintenanceMode') END
            || CASE WHEN w.specifications ? 'isAllowAnySourceCors' OR NOT w.specifications ? 'IsAllowAnySourceCors' THEN '{}'::jsonb ELSE jsonb_build_object('isAllowAnySourceCors',w.specifications->'IsAllowAnySourceCors') END
            || jsonb_build_object('schemaVersion',2)) ELSE w.specifications END AS specifications,
           CASE WHEN w.proxy_rules IS NULL OR jsonb_typeof(w.proxy_rules)='null' THEN '[]'::jsonb WHEN jsonb_typeof(w.proxy_rules)='array' THEN COALESCE((SELECT jsonb_agg(CASE WHEN jsonb_typeof(r.rule)='object' THEN
             (r.rule - ARRAY['Name','Description','RuleId','IsEnabled','PathPattern','IsRegexMatchPattern','Destination','DestinationHost','RemovePrefix','IsWithOriginalHostHeader','IsAllowUnsafeSslCertificate','WebProxy'])
             || CASE WHEN r.rule ? 'name' OR NOT r.rule ? 'Name' THEN '{}'::jsonb ELSE jsonb_build_object('name',r.rule->'Name') END
             || CASE WHEN r.rule ? 'description' OR NOT r.rule ? 'Description' THEN '{}'::jsonb ELSE jsonb_build_object('description',r.rule->'Description') END
             || CASE WHEN r.rule ? 'ruleId' OR NOT r.rule ? 'RuleId' THEN '{}'::jsonb ELSE jsonb_build_object('ruleId',r.rule->'RuleId') END
             || CASE WHEN r.rule ? 'isEnabled' OR NOT r.rule ? 'IsEnabled' THEN '{}'::jsonb ELSE jsonb_build_object('isEnabled',r.rule->'IsEnabled') END
             || CASE WHEN r.rule ? 'pathPattern' OR NOT r.rule ? 'PathPattern' THEN '{}'::jsonb ELSE jsonb_build_object('pathPattern',r.rule->'PathPattern') END
             || CASE WHEN r.rule ? 'isRegexMatchPattern' OR NOT r.rule ? 'IsRegexMatchPattern' THEN '{}'::jsonb ELSE jsonb_build_object('isRegexMatchPattern',r.rule->'IsRegexMatchPattern') END
             || CASE WHEN r.rule ? 'destination' OR NOT r.rule ? 'Destination' THEN '{}'::jsonb ELSE jsonb_build_object('destination',r.rule->'Destination') END
             || CASE WHEN r.rule ? 'destinationHost' OR NOT r.rule ? 'DestinationHost' THEN '{}'::jsonb ELSE jsonb_build_object('destinationHost',r.rule->'DestinationHost') END
             || CASE WHEN r.rule ? 'removePrefix' OR NOT r.rule ? 'RemovePrefix' THEN '{}'::jsonb ELSE jsonb_build_object('removePrefix',r.rule->'RemovePrefix') END
             || CASE WHEN r.rule ? 'preserveOriginalHost' OR NOT r.rule ? 'IsWithOriginalHostHeader' THEN '{}'::jsonb ELSE jsonb_build_object('preserveOriginalHost',r.rule->'IsWithOriginalHostHeader') END
             || CASE WHEN r.rule ? 'allowInvalidTlsCertificate' OR NOT r.rule ? 'IsAllowUnsafeSslCertificate' THEN '{}'::jsonb ELSE jsonb_build_object('allowInvalidTlsCertificate',r.rule->'IsAllowUnsafeSslCertificate') END
             || CASE WHEN r.rule ? 'webProxy' OR NOT r.rule ? 'WebProxy' THEN '{}'::jsonb ELSE jsonb_build_object('webProxy',r.rule->'WebProxy') END
             ELSE r.rule END
           ) FROM jsonb_array_elements(w.proxy_rules) r(rule)), '[]'::jsonb) ELSE w.proxy_rules END AS proxy_rules,
           CASE WHEN w.proxy_transforms IS NULL OR jsonb_typeof(w.proxy_transforms)='null' THEN '[]'::jsonb WHEN jsonb_typeof(w.proxy_transforms)='object' THEN COALESCE((SELECT jsonb_agg(jsonb_build_object('key',k,'value',CASE WHEN jsonb_typeof(v)='string' THEN v #>> '{}' ELSE v::text END)) FROM jsonb_each(w.proxy_transforms) e(k,v)), '[]'::jsonb) ELSE w.proxy_transforms END AS proxy_transforms,
           CASE WHEN w.simple_responses IS NULL OR jsonb_typeof(w.simple_responses)='null' THEN '[]'::jsonb WHEN jsonb_typeof(w.simple_responses)='array' THEN COALESCE((SELECT jsonb_agg(CASE WHEN jsonb_typeof(s.item)='object' THEN
             (s.item - ARRAY['Url','ContentType','StatusCode','ResponseContent','ExtraHeaders'])
             || CASE WHEN s.item ? 'path' OR NOT s.item ? 'Url' THEN '{}'::jsonb ELSE jsonb_build_object('path',s.item->'Url') END
             || CASE WHEN s.item ? 'contentType' OR NOT s.item ? 'ContentType' THEN '{}'::jsonb ELSE jsonb_build_object('contentType',s.item->'ContentType') END
             || CASE WHEN s.item ? 'statusCode' OR NOT s.item ? 'StatusCode' THEN '{}'::jsonb ELSE jsonb_build_object('statusCode',s.item->'StatusCode') END
             || CASE WHEN s.item ? 'responseContent' OR NOT s.item ? 'ResponseContent' THEN '{}'::jsonb ELSE jsonb_build_object('responseContent',s.item->'ResponseContent') END
             || CASE WHEN s.item ? 'extraHeaders' OR NOT s.item ? 'ExtraHeaders' THEN '{}'::jsonb ELSE jsonb_build_object('extraHeaders',CASE WHEN jsonb_typeof(s.item->'ExtraHeaders')='array' THEN COALESCE((SELECT jsonb_agg((h.item - ARRAY['Key','Value']) || CASE WHEN h.item ? 'key' OR NOT h.item ? 'Key' THEN '{}'::jsonb ELSE jsonb_build_object('key',h.item->'Key') END || CASE WHEN h.item ? 'value' OR NOT h.item ? 'Value' THEN '{}'::jsonb ELSE jsonb_build_object('value',h.item->'Value') END) FROM jsonb_array_elements(s.item->'ExtraHeaders') h(item)),'[]'::jsonb) ELSE s.item->'ExtraHeaders' END) END
             ELSE s.item END
           ) FROM jsonb_array_elements(w.simple_responses) s(item)), '[]'::jsonb) ELSE w.simple_responses END AS simple_responses,
           (jsonb_typeof(COALESCE(w.specifications,'{}'::jsonb))='object'
            AND NOT EXISTS (SELECT 1 FROM jsonb_object_keys(COALESCE(w.specifications,'{}'::jsonb)) k(key) WHERE k.key NOT IN ('id','name','schemaVersion','defaultHost','defaultDestination','defaultDestinationHost','webProxy','authorizationPolicy','preserveOriginalHost','allowInvalidTlsCertificate','redirectHttpToHttps','isMaintenanceMode','isAllowAnySourceCors','ipAccessMode','ipAccessCIDRs','hosts','proxyRules','proxyTransforms','simpleResponses','DefaultHost','DefaultDestination','DefaultDestinationHost','WebProxy','AuthorizationPolicy','IsWithOriginalHostHeader','IsAllowUnsafeSslCertificate','IsAutoRedirectHttp2Https','IsMaintenanceMode','IsAllowAnySourceCors'))
            AND (w.proxy_rules IS NULL OR jsonb_typeof(w.proxy_rules) IN ('array','null'))
            AND CASE WHEN jsonb_typeof(w.proxy_rules)='array' THEN NOT EXISTS (SELECT 1 FROM jsonb_array_elements(w.proxy_rules) e(item) WHERE jsonb_typeof(e.item)<>'object') ELSE true END
            AND CASE WHEN jsonb_typeof(w.proxy_rules)='array' THEN NOT EXISTS (SELECT 1 FROM jsonb_array_elements(w.proxy_rules) e(item) CROSS JOIN LATERAL jsonb_object_keys(e.item) k(key) WHERE k.key NOT IN ('ruleId','name','description','isEnabled','pathPattern','isRegexMatchPattern','destination','destinationHost','preserveOriginalHost','allowInvalidTlsCertificate','webProxy','removePrefix','RuleId','Name','Description','IsEnabled','PathPattern','IsRegexMatchPattern','Destination','DestinationHost','IsWithOriginalHostHeader','IsAllowUnsafeSslCertificate','WebProxy','RemovePrefix')) ELSE true END
            AND (w.proxy_transforms IS NULL OR jsonb_typeof(w.proxy_transforms) IN ('array','object','null'))
            AND (w.simple_responses IS NULL OR jsonb_typeof(w.simple_responses) IN ('array','null'))
            AND CASE WHEN jsonb_typeof(w.simple_responses)='array' THEN NOT EXISTS (SELECT 1 FROM jsonb_array_elements(w.simple_responses) e(item) WHERE jsonb_typeof(e.item)<>'object' OR (e.item ? 'ExtraHeaders' AND jsonb_typeof(e.item->'ExtraHeaders')<>'array')
                OR (jsonb_typeof(e.item->'ExtraHeaders')='array' AND EXISTS (SELECT 1 FROM jsonb_array_elements(e.item->'ExtraHeaders') h(item) WHERE jsonb_typeof(h.item)<>'object' OR EXISTS (SELECT 1 FROM jsonb_object_keys(h.item) k(key) WHERE k.key NOT IN ('key','value','Key','Value'))))
                OR EXISTS (SELECT 1 FROM jsonb_object_keys(e.item) k(key) WHERE k.key NOT IN ('path','contentType','statusCode','responseContent','extraHeaders','Url','ContentType','StatusCode','ResponseContent','ExtraHeaders'))) ELSE true END) AS valid
    FROM website w WHERE w.data_schema_version < 2
), applied AS (
    UPDATE website w SET specifications=n.specifications, proxy_rules=n.proxy_rules, proxy_transforms=n.proxy_transforms, simple_responses=n.simple_responses, data_schema_version=2, update_time=clock_timestamp()
    FROM normalized n
    WHERE w.id=n.id AND n.valid AND jsonb_typeof(n.specifications)='object' AND n.specifications->>'schemaVersion'='2'
      AND jsonb_typeof(n.proxy_rules)='array' AND jsonb_typeof(n.proxy_transforms)='array' AND jsonb_typeof(n.simple_responses)='array'
    RETURNING w.id
)
INSERT INTO website_config_migration_error(website_id,reason,specifications,hosts,proxy_rules,proxy_transforms,simple_responses)
SELECT n.id,'unsupported or invalid website collection shape',w.specifications,w.hosts,w.proxy_rules,w.proxy_transforms,w.simple_responses
FROM normalized n JOIN website w ON w.id=n.id
WHERE NOT EXISTS (SELECT 1 FROM applied a WHERE a.id=n.id)
ON CONFLICT (website_id) DO UPDATE SET reason=excluded.reason, recorded_at=clock_timestamp();

-- +goose Down
UPDATE website SET data_schema_version=1 WHERE data_schema_version=2;
