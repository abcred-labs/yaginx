// fixtureseed owns the short-lived account used to collect W0 golden fixtures.
// It is deliberately constrained to a run-prefixed account so cleanup cannot
// affect ordinary development users.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var runPattern = regexp.MustCompile(`^[a-z0-9-]{3,40}$`)

const (
	fixtureWebsiteID            int64 = 907260000000001
	fixtureWebDomainID          int64 = 907260000000002
	fixtureRequestHistoryID     int64 = 907260000000003
	fixtureDeleteWebsiteID      int64 = 907260000000004
	fixtureDeleteDomainID       int64 = 907260000000005
	fixtureProxyRulesID         int64 = 907260000000006
	fixtureHostOverrideID       int64 = 907260000000007
	fixtureRedirectID           int64 = 907260000000008
	fixturePathPrefixID         int64 = 907260000000009
	fixturePathSetID            int64 = 907260000000010
	fixtureQuerySetID           int64 = 907260000000011
	fixtureRequestHeaderID      int64 = 907260000000012
	fixtureOriginalHostID       int64 = 907260000000013
	fixtureResponseHeaderID     int64 = 907260000000014
	fixtureChildNoTransformID   int64 = 907260000000015
	fixturePathRemoveID         int64 = 907260000000016
	fixtureRequestCopyID        int64 = 907260000000017
	fixtureResponseCopyID       int64 = 907260000000018
	fixtureForwardProxyID       int64 = 907260000000019
	fixtureUnsafeTLSID          int64 = 907260000000020
	fixtureChildTransportID     int64 = 907260000000021
	fixtureRefusedDestinationID int64 = 907260000000022
	fixtureXForwardedID         int64 = 907260000000023
	fixtureForwardedID          int64 = 907260000000024
	fixtureResponseTrailerID    int64 = 907260000000025
	fixtureTrailerCopyID        int64 = 907260000000026
	fixturePathPatternID        int64 = 907260000000027
	fixtureQueryRouteID         int64 = 907260000000028
	fixtureSSEID                int64 = 907260000000029
	fixtureChunkedID            int64 = 907260000000030
	fixtureLargeFileID          int64 = 907260000000031
	fixtureWebSocketID          int64 = 907260000000032
	fixtureRawHeaderFailureID   int64 = 907260000000033
	fixtureRawBodyFailureID     int64 = 907260000000034
)

func main() {
	action := flag.String("action", "seed", "seed, seed-data, or cleanup")
	run := flag.String("run", "", "lowercase run identifier")
	dsnEnv := flag.String("dsn-env", "YAGINX_FIXTURE_DATABASE_URL", "environment variable holding the PostgreSQL DSN")
	passwordEnv := flag.String("password-env", "YAGINX_FIXTURE_PASSWORD", "environment variable holding the fixture account password")
	flag.Parse()
	if !runPattern.MatchString(*run) {
		fatal("-run must match %s", runPattern.String())
	}
	dsn := os.Getenv(*dsnEnv)
	if dsn == "" {
		fatal("%s is not set", *dsnEnv)
	}
	email := "w0-fixture-" + *run + "@example.invalid"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fatal("connect: %v", err)
	}
	defer connection.Close(context.Background())
	switch *action {
	case "seed":
		password := os.Getenv(*passwordEnv)
		if password == "" {
			fatal("%s is not set", *passwordEnv)
		}
		id, err := seed(ctx, connection, email, password)
		if err != nil {
			fatal("seed: %v", err)
		}
		fmt.Printf("seeded fixture account id=%d email=%s\n", id, email)
	case "seed-data":
		if err := seedData(ctx, connection, *run); err != nil {
			fatal("seed data: %v", err)
		}
		fmt.Printf("seeded fixture data run=%s\n", *run)
	case "cleanup":
		if err := cleanup(ctx, connection, email, *run); err != nil {
			fatal("cleanup: %v", err)
		}
		fmt.Printf("cleaned fixture account email=%s\n", email)
	default:
		fatal("-action must be seed or cleanup")
	}
}

func seed(ctx context.Context, connection *pgx.Conn, email, password string) (int64, error) {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback(context.Background())
	var exists bool
	if err := transaction.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM account WHERE email = $1)", email).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, fmt.Errorf("fixture account already exists; run cleanup first")
	}
	var id int64
	if err := transaction.QueryRow(ctx, `
INSERT INTO account (email, password, password_hash, password_salt)
VALUES ($1, $2, '', '')
RETURNING id`, email, password).Scan(&id); err != nil {
		return 0, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// seedData creates only deterministic, prefix-scoped rows. The values are
// deliberately synthetic and cleanup removes them by fixed IDs/prefix before
// touching the short-lived account.
func seedData(ctx context.Context, connection *pgx.Conn, run string) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(context.Background())
	name := "w0-fixture-" + run
	domain := name + ".example.invalid"
	const proxyHost = "proxy-fixture.example.invalid"
	const proxyDestination = "http://127.0.0.1:20180"
	const timestamp = "2020-01-02T03:04:05Z"
	const reportTS int64 = 1577934245
	specifications := `{"defaultHost":"` + proxyHost + `","defaultDestination":"` + proxyDestination + `","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`
	simpleResponses := `[{"path":"/simple","contentType":"text/plain; charset=utf-8","statusCode":218,"responseContent":"fixture-simple-response","extraHeaders":[{"key":"X-Fixture-Header","value":"fixture-value"}]}]`
	proxyRules := `[{"ruleId":907260000000101,"name":"fixture-segment-rule","description":"","isEnabled":true,"pathPattern":"rule","isRegexMatchPattern":false,"destination":"http://127.0.0.1:20180","destinationHost":"","allowInvalidTlsCertificate":false,"webProxy":"","removePrefix":""},{"ruleId":907260000000102,"name":"fixture-regex-ignored","description":"","isEnabled":true,"pathPattern":"regex","isRegexMatchPattern":true,"destination":"http://127.0.0.1:20180","destinationHost":"","allowInvalidTlsCertificate":false,"webProxy":"","removePrefix":""},{"ruleId":907260000000103,"name":"fixture-disabled","description":"","isEnabled":false,"pathPattern":"disabled","isRegexMatchPattern":false,"destination":"http://127.0.0.1:20180","destinationHost":"","allowInvalidTlsCertificate":false,"webProxy":"","removePrefix":""},{"ruleId":907260000000104,"name":"fixture-remove-prefix","description":"","isEnabled":true,"pathPattern":"remove","isRegexMatchPattern":false,"destination":"http://127.0.0.1:20180","destinationHost":"","allowInvalidTlsCertificate":false,"webProxy":"","removePrefix":"/remove"},{"ruleId":907260000000105,"name":"fixture-legacy-destination","description":"","isEnabled":true,"pathPattern":"legacy-destination","isRegexMatchPattern":false,"destination":"","destinationHost":"http://127.0.0.1:20180","allowInvalidTlsCertificate":false,"webProxy":"","removePrefix":""}]`
	if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1, $2, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4::timestamptz, $4::timestamptz, '{}'::jsonb, $5::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, fixtureWebsiteID, name, specifications, timestamp, simpleResponses); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1, $2, $3::jsonb, '["proxy-rules-alias.example.invalid", ""]'::jsonb, $4::jsonb, $5::timestamptz, $5::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, fixtureProxyRulesID, name+"-proxy-rules", `{"defaultHost":"proxy-rules-fixture.example.invalid","defaultDestination":"`+proxyDestination+`","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`, proxyRules, timestamp); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1, $2, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4::timestamptz, $4::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, fixtureHostOverrideID, name+"-host-override", `{"defaultHost":"proxy-host-override-fixture.example.invalid","defaultDestination":"`+proxyDestination+`","defaultDestinationHost":"fixture-upstream-host.example.invalid","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`, timestamp); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1, $2, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4::timestamptz, $4::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, fixtureRedirectID, name+"-redirect", `{"defaultHost":"proxy-redirect-fixture.example.invalid","defaultDestination":"`+proxyDestination+`","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":true}`, timestamp); err != nil {
		return err
	}
	transformSites := []struct {
		id, host, transforms string
	}{
		{fmt.Sprint(fixturePathPrefixID), "proxy-transform-prefix-fixture.example.invalid", `{"PathPrefix":"/fixture-prefix"}`},
		{fmt.Sprint(fixturePathSetID), "proxy-transform-set-fixture.example.invalid", `{"PathSet":"/fixture-set"}`},
		{fmt.Sprint(fixtureQuerySetID), "proxy-transform-query-fixture.example.invalid", `{"QueryValueParameter":"fixture","Set":"transformed"}`},
		{fmt.Sprint(fixtureRequestHeaderID), "proxy-transform-request-header-fixture.example.invalid", `{"RequestHeader":"X-Fixture","Set":"transformed"}`},
		{fmt.Sprint(fixtureOriginalHostID), "proxy-original-host-fixture.example.invalid", `{}`},
		{fmt.Sprint(fixtureResponseHeaderID), "proxy-transform-response-header-fixture.example.invalid", `{"ResponseHeader":"X-Fixture-Response","Set":"transformed"}`},
		{fmt.Sprint(fixtureXForwardedID), "proxy-transform-x-forwarded-fixture.example.invalid", `{"X-Forwarded":"Set","For":"Set","Host":"Set","Proto":"Set","Prefix":"Set"}`},
		{fmt.Sprint(fixtureForwardedID), "proxy-transform-forwarded-fixture.example.invalid", `{"Forwarded":"for,proto,host","ForFormat":"Ip","Action":"Set"}`},
		{fmt.Sprint(fixtureResponseTrailerID), "proxy-transform-response-trailer-fixture.example.invalid", `{"ResponseTrailer":"X-Fixture-Added","Set":"transformed","When":"Always"}`},
		{fmt.Sprint(fixtureTrailerCopyID), "proxy-transform-trailer-copy-fixture.example.invalid", `{"ResponseTrailersCopy":"false"}`},
		{fmt.Sprint(fixturePathPatternID), "proxy-transform-path-pattern-fixture.example.invalid", `{"PathPattern":"/mapped/{**remainder}"}`},
		{fmt.Sprint(fixtureQueryRouteID), "proxy-transform-query-route-fixture.example.invalid", `{"QueryRouteParameter":"route","Set":"remainder"}`},
		{fmt.Sprint(fixtureSSEID), "proxy-stream-sse-fixture.example.invalid", `{}`},
		{fmt.Sprint(fixtureChunkedID), "proxy-stream-chunked-fixture.example.invalid", `{}`},
		{fmt.Sprint(fixtureLargeFileID), "proxy-stream-large-fixture.example.invalid", `{}`},
		{fmt.Sprint(fixtureWebSocketID), "proxy-stream-websocket-fixture.example.invalid", `{}`},
	}
	for _, site := range transformSites {
		isOriginalHost := site.host == "proxy-original-host-fixture.example.invalid"
		spec := `{"defaultHost":"` + site.host + `","defaultDestination":"` + proxyDestination + `","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":` + fmt.Sprint(isOriginalHost) + `,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`
		if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1::bigint, $2, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4::timestamptz, $4::timestamptz, $5::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, site.id, name+"-"+site.id, spec, timestamp, site.transforms); err != nil {
			return err
		}
	}
	childRules := `[{"ruleId":907260000000151,"name":"fixture-child-no-transform","description":"","isEnabled":true,"pathPattern":"child","isRegexMatchPattern":false,"destination":"http://127.0.0.1:20180","destinationHost":"","allowInvalidTlsCertificate":false,"webProxy":"","removePrefix":""}]`
	additionalSites := []struct {
		id, host, transforms, rules string
	}{
		{fmt.Sprint(fixtureChildNoTransformID), "proxy-child-no-transform-fixture.example.invalid", `{"PathPrefix":"/fixture-prefix"}`, childRules},
		{fmt.Sprint(fixturePathRemoveID), "proxy-transform-remove-fixture.example.invalid", `{"PathRemovePrefix":"/fixture-remove"}`, `[]`},
		{fmt.Sprint(fixtureRequestCopyID), "proxy-transform-request-copy-fixture.example.invalid", `{"RequestHeadersCopy":"false"}`, `[]`},
		{fmt.Sprint(fixtureResponseCopyID), "proxy-transform-response-copy-fixture.example.invalid", `{"ResponseHeadersCopy":"false"}`, `[]`},
	}
	for _, site := range additionalSites {
		spec := `{"defaultHost":"` + site.host + `","defaultDestination":"` + proxyDestination + `","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`
		if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1::bigint, $2, $3::jsonb, '[]'::jsonb, $4::jsonb, $5::timestamptz, $5::timestamptz, $6::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, site.id, name+"-"+site.id, spec, site.rules, timestamp, site.transforms); err != nil {
			return err
		}
	}
	transportSites := []struct {
		id, host, spec, rules string
	}{
		{fmt.Sprint(fixtureForwardProxyID), "proxy-forward-proxy-fixture.example.invalid", `{"defaultHost":"proxy-forward-proxy-fixture.example.invalid","defaultDestination":"` + proxyDestination + `","defaultDestinationHost":"","webProxy":"http://127.0.0.1:20181","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`, `[]`},
		{fmt.Sprint(fixtureUnsafeTLSID), "proxy-unsafe-tls-fixture.example.invalid", `{"defaultHost":"proxy-unsafe-tls-fixture.example.invalid","defaultDestination":"https://127.0.0.1:20182","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":true,"redirectHttpToHttps":false}`, `[]`},
		{fmt.Sprint(fixtureChildTransportID), "proxy-child-transport-fixture.example.invalid", `{"defaultHost":"proxy-child-transport-fixture.example.invalid","defaultDestination":"` + proxyDestination + `","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`, `[{"ruleId":907260000000211,"name":"fixture-child-forward-proxy","description":"","isEnabled":true,"pathPattern":"web-proxy","isRegexMatchPattern":false,"destination":"http://127.0.0.1:20180","destinationHost":"","allowInvalidTlsCertificate":false,"webProxy":"http://127.0.0.1:20181","removePrefix":""},{"ruleId":907260000000212,"name":"fixture-child-unsafe-tls","description":"","isEnabled":true,"pathPattern":"unsafe-tls","isRegexMatchPattern":false,"destination":"https://127.0.0.1:20182","destinationHost":"","allowInvalidTlsCertificate":true,"webProxy":"","removePrefix":""}]`},
	}
	for _, site := range transportSites {
		if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1::bigint, $2, $3::jsonb, '[]'::jsonb, $4::jsonb, $5::timestamptz, $5::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, site.id, name+"-"+site.id, site.spec, site.rules, timestamp); err != nil {
			return err
		}
	}
	rawFailureSites := []struct {
		id, host, destination string
	}{
		{fmt.Sprint(fixtureRawHeaderFailureID), "proxy-raw-header-failure-fixture.example.invalid", "http://127.0.0.1:20184"},
		{fmt.Sprint(fixtureRawBodyFailureID), "proxy-raw-body-failure-fixture.example.invalid", "http://127.0.0.1:20185"},
	}
	for _, site := range rawFailureSites {
		spec := `{"defaultHost":"` + site.host + `","defaultDestination":"` + site.destination + `","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`
		if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1::bigint, $2, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4::timestamptz, $4::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, site.id, name+"-"+site.id, spec, timestamp); err != nil {
			return err
		}
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1, $2, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4::timestamptz, $4::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, fixtureRefusedDestinationID, name+"-refused-destination", `{"defaultHost":"proxy-refused-destination-fixture.example.invalid","defaultDestination":"http://127.0.0.1:20183","defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`, timestamp); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO web_domain (id, name, is_use_free_cert, is_verified, free_cert_message)
VALUES ($1, $2, false, false, '')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, is_use_free_cert = EXCLUDED.is_use_free_cert, is_verified = EXCLUDED.is_verified, free_cert_message = EXCLUDED.free_cert_message`, fixtureWebDomainID, domain); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO website (id, name, specifications, hosts, proxy_rules, create_time, update_time, proxy_transforms, simple_responses)
VALUES ($1, $2, '{}'::jsonb, '[]'::jsonb, '[]'::jsonb, $3::timestamptz, $3::timestamptz, '{}'::jsonb, '[]'::jsonb)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, specifications = EXCLUDED.specifications, hosts = EXCLUDED.hosts, proxy_rules = EXCLUDED.proxy_rules, create_time = EXCLUDED.create_time, update_time = EXCLUDED.update_time, proxy_transforms = EXCLUDED.proxy_transforms, simple_responses = EXCLUDED.simple_responses`, fixtureDeleteWebsiteID, name+"-delete", timestamp); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO web_domain (id, name, is_use_free_cert, is_verified, free_cert_message)
VALUES ($1, $2, false, false, '')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, is_use_free_cert = EXCLUDED.is_use_free_cert, is_verified = EXCLUDED.is_verified, free_cert_message = EXCLUDED.free_cert_message`, fixtureDeleteDomainID, name+"-delete.example.invalid"); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO request_history (id, request_time, request_ts, duration_ms, client_ip, method, scheme, host, path, query_string, url, protocol, status_code, user_agent, proxy_stage, exception_stage, forwarder_error, request_headers, response_headers, request_body_base64, response_body_base64, request_content_type, response_content_type, request_body_truncated, response_body_truncated, exception_type, exception_message, exception_stack_trace, upstream_status_code, status_source, error_category, error_code)
VALUES ($1, $2::timestamptz, $3, 1, '', 'GET', 'http', $4, '/', '', '', 'HTTP/1.1', 200, '', '', '', '', '{}'::jsonb, '{}'::jsonb, '', '', '', '', false, false, '', '', '', 200, 'upstream', '', '')
ON CONFLICT (id) DO UPDATE SET host = EXCLUDED.host, request_time = EXCLUDED.request_time, request_ts = EXCLUDED.request_ts`, fixtureRequestHistoryID, timestamp, reportTS, name); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO host_traffic (id, host_name, period, request_counts, inbound_bytes, outbound_bytes)
VALUES ($1, $2, $3, 1, 2, 3)
ON CONFLICT (id) DO UPDATE SET host_name = EXCLUDED.host_name, period = EXCLUDED.period, request_counts = EXCLUDED.request_counts, inbound_bytes = EXCLUDED.inbound_bytes, outbound_bytes = EXCLUDED.outbound_bytes`, fixtureRequestHistoryID, name, reportTS); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO resource_report (resource_uuid, cycle_type, report_time, report_ts, request_qty, status_code, spider, browser, os, duration, create_time, update_time)
VALUES ($1, 5, $2::timestamptz, $3, 1, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, $2::timestamptz, $2::timestamptz)
ON CONFLICT (resource_uuid, cycle_type, report_ts) DO UPDATE SET request_qty = EXCLUDED.request_qty, update_time = EXCLUDED.update_time`, name, timestamp, reportTS); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func cleanup(ctx context.Context, connection *pgx.Conn, email, run string) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(context.Background())
	name := "w0-fixture-" + run
	if _, err := transaction.Exec(ctx, "DELETE FROM resource_report WHERE resource_uuid = $1", name); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM host_traffic WHERE id = $1 AND host_name = $2", fixtureRequestHistoryID, name); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM request_history WHERE id = $1 AND host = $2", fixtureRequestHistoryID, name); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM website WHERE id = $1 AND name = $2", fixtureWebsiteID, name); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM website WHERE id = $1 AND name = $2", fixtureDeleteWebsiteID, name+"-delete"); err != nil {
		return err
	}
	for _, id := range []int64{fixtureProxyRulesID, fixtureHostOverrideID, fixtureRedirectID, fixturePathPrefixID, fixturePathSetID, fixtureQuerySetID, fixtureRequestHeaderID, fixtureOriginalHostID, fixtureResponseHeaderID, fixtureChildNoTransformID, fixturePathRemoveID, fixtureRequestCopyID, fixtureResponseCopyID, fixtureForwardProxyID, fixtureUnsafeTLSID, fixtureChildTransportID, fixtureRefusedDestinationID, fixtureXForwardedID, fixtureForwardedID, fixtureResponseTrailerID, fixtureTrailerCopyID, fixturePathPatternID, fixtureQueryRouteID, fixtureSSEID, fixtureChunkedID, fixtureLargeFileID, fixtureWebSocketID, fixtureRawHeaderFailureID, fixtureRawBodyFailureID} {
		if _, err := transaction.Exec(ctx, "DELETE FROM website WHERE id = $1 AND name LIKE $2", id, name+"-%"); err != nil {
			return err
		}
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM web_domain WHERE id = $1 AND name = $2", fixtureWebDomainID, name+".example.invalid"); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM web_domain WHERE id = $1 AND name = $2", fixtureDeleteDomainID, name+"-delete.example.invalid"); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM account_password_recovery WHERE account_email LIKE $1", name+"%@example.invalid"); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM account WHERE email LIKE $1", name+"%@example.invalid"); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fixtureseed: "+format+"\n", args...)
	os.Exit(1)
}
