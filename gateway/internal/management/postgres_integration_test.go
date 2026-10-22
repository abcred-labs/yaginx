//go:build integration

package management_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/auth"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
	managementserver "yaginx-gateway/internal/management/server"
	"yaginx-gateway/internal/proxy"
	"yaginx-gateway/internal/repository"
	"yaginx-gateway/internal/telemetry"
)

func TestPostgresManagementReachableFlows(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := uniqueSchema(t)
	t.Logf("isolated schema=%s", schema)
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	var migrationSchema string
	if err := base.QueryRow(ctx, "SELECT table_schema FROM information_schema.tables WHERE table_schema=$1 AND table_name='yaginx_schema_migration'", schema).Scan(&migrationSchema); err != nil || migrationSchema != schema {
		t.Fatalf("v2 migration tracker escaped isolated schema: %q err=%v", migrationSchema, err)
	}
	config, err := pgxpool.ParseConfig(isolated)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "INSERT INTO account(email,password,password_hash,password_salt) VALUES($1,$2,'','')", "admin@example.invalid", "old-password"); err != nil {
		t.Fatal(err)
	}
	store := repository.New(pool)
	mail := newFakeSMTP(t)
	defer mail.Close()
	smtpHost := "127.0.0.1"
	smtpFromEmail := "no-reply@example.invalid"
	smtpFromName := "Yaginx"
	if err := store.SetSMTP(ctx, management.SMTPSettings{Enabled: true, Host: &smtpHost, Port: mail.Port(), FromEmail: &smtpFromEmail, FromName: &smtpFromName}); err != nil {
		t.Fatal(err)
	}
	keys := auth.Keys{Current: []byte("01234567890123456789012345678901"), Issuer: "Yaginx Proxy Server", Audience: "Yaingx Clients", Session: 10 * time.Minute}
	router := managementserver.New(managementserver.Options{
		Store:        store,
		SystemErrors: store,
		Keys:         keys,
		RecordSystemError: func(_ context.Context, _, _, _, _ string, err error) {
			t.Logf("management store error: %v", err)
		},
	})
	login := call(t, router, http.MethodPost, "/api/account/login", nil, map[string]any{"email": "admin@example.invalid", "password": "old-password"})
	if login.Code != http.StatusOK {
		t.Fatalf("login=%#v", login)
	}
	var migratedPlain *string
	var migratedHash, migratedSalt string
	if err := pool.QueryRow(ctx, "SELECT password,password_hash,password_salt FROM account WHERE email=$1", "admin@example.invalid").Scan(&migratedPlain, &migratedHash, &migratedSalt); err != nil || migratedPlain != nil || migratedHash == "" || migratedSalt == "" {
		t.Fatalf("legacy login migration plain=%#v hash=%q salt=%q err=%v", migratedPlain, migratedHash, migratedSalt, err)
	}
	data := login.Data.(map[string]any)
	token, _ := data["token"].(string)
	if token == "" {
		t.Fatalf("missing token: %#v", data)
	}
	start := call(t, router, http.MethodPost, "/api/account/password_recovery/start", nil, map[string]any{"accountEmail": "admin@example.invalid"})
	if start.Code != http.StatusOK {
		t.Fatalf("start=%#v", start)
	}
	recoveryID := start.Data.(map[string]any)["recoveryId"].(string)
	verify := call(t, router, http.MethodPost, "/api/account/password_recovery/verify_email", nil, map[string]any{"recoveryId": recoveryID, "email": "admin@example.invalid"})
	if verify.Code != http.StatusOK {
		t.Fatalf("verify=%#v", verify)
	}
	code := mail.Code(t)
	reset := call(t, router, http.MethodPost, "/api/account/password_recovery/reset", nil, map[string]any{"recoveryId": recoveryID, "code": code, "newPassword": "new-password", "confirmPassword": "new-password"})
	if reset.Code != http.StatusOK {
		t.Fatalf("reset=%#v", reset)
	}
	if got := call(t, router, http.MethodPost, "/api/account/login", nil, map[string]any{"email": "admin@example.invalid", "password": "new-password"}); got.Code != http.StatusOK {
		t.Fatalf("reset login=%#v", got)
	}
	authz := map[string]string{"Authorization": "Bearer " + token}
	registeredEmail := "registered@example.invalid"
	if got := call(t, router, http.MethodPost, "/api/account/register", authz, map[string]any{"email": registeredEmail, "password": "registered-password"}); got.Code != http.StatusOK || !isEmptyObject(got.Data) {
		t.Fatalf("account register=%#v", got)
	}
	registeredLogin := call(t, router, http.MethodPost, "/api/account/login", nil, map[string]any{"email": registeredEmail, "password": "registered-password"})
	if registeredLogin.Code != http.StatusOK {
		t.Fatalf("registered login=%#v", registeredLogin)
	}
	registeredToken := registeredLogin.Data.(map[string]any)["token"].(string)
	if got := call(t, router, http.MethodPost, "/api/account/update_profile", map[string]string{"Authorization": "Bearer " + registeredToken}, map[string]any{"oldPassword": "registered-password", "newPassword": "updated-password", "confirmPassword": "updated-password"}); got.Code != http.StatusOK || got.Data != true {
		t.Fatalf("profile update=%#v", got)
	}
	if got := call(t, router, http.MethodPost, "/api/account/login", nil, map[string]any{"email": registeredEmail, "password": "updated-password"}); got.Code != http.StatusOK {
		t.Fatalf("updated registered login=%#v", got)
	}
	var plain *string
	var passwordHash, passwordSalt string
	if err := pool.QueryRow(ctx, "SELECT password,password_hash,password_salt FROM account WHERE email=$1", registeredEmail).Scan(&plain, &passwordHash, &passwordSalt); err != nil || plain != nil || passwordHash == "" || passwordSalt == "" {
		t.Fatalf("registered credentials plain=%#v hash=%q salt=%q err=%v", plain, passwordHash, passwordSalt, err)
	}
	accounts, err := store.ListAccounts(ctx)
	if err != nil || len(accounts) != 2 || accounts[0].ID >= accounts[1].ID || accounts[0].Email != "admin@example.invalid" || accounts[1].Email != registeredEmail {
		t.Fatalf("account list=%#v err=%v", accounts, err)
	}
	if err := store.ResetAccountPassword(ctx, accounts[1].ID, "cli-reset-password"); err != nil {
		t.Fatalf("reset account password: %v", err)
	}
	if _, err := store.Login(ctx, registeredEmail, "updated-password"); !errors.Is(err, management.ErrNotFound) {
		t.Fatalf("old password login error=%v", err)
	}
	if _, err := store.Login(ctx, registeredEmail, "cli-reset-password"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
	var resetPlain *string
	var resetHash, resetSalt string
	if err := pool.QueryRow(ctx, "SELECT password,password_hash,password_salt FROM account WHERE id=$1", accounts[1].ID).Scan(&resetPlain, &resetHash, &resetSalt); err != nil || resetPlain != nil || resetHash == "" || resetSalt == "" || resetHash == passwordHash || resetSalt == passwordSalt {
		t.Fatalf("reset credentials plain=%#v hash=%q salt=%q err=%v", resetPlain, resetHash, resetSalt, err)
	}
	if err := store.ResetAccountPassword(ctx, 999999999, "unused-password"); !errors.Is(err, management.ErrNotFound) {
		t.Fatalf("missing account reset error=%v", err)
	}
	website := call(t, router, http.MethodPost, "/api/website/upsert", authz, map[string]any{"name": "integration-site", "hosts": []string{"integration.example.invalid"}, "defaultDestination": "https://upstream.example.invalid", "simpleResponses": []any{map[string]any{"path": "/simple", "statusCode": 218, "contentType": "text/plain", "responseContent": "integration", "extraHeaders": []any{map[string]any{"key": "X-Integration", "value": "present"}}}}})
	if website.Code != http.StatusOK {
		t.Fatalf("website=%#v", website)
	}
	websiteID := fmt.Sprint(website.Data.(map[string]any)["id"])
	var managedDomainID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM web_domain WHERE name=$1", "integration.example.invalid").Scan(&managedDomainID); err != nil {
		t.Fatalf("managed domain: %v", err)
	}
	if got := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{}); got.Code != http.StatusOK {
		t.Fatalf("website search=%#v", got)
	}
	if got := call(t, router, http.MethodGet, "/api/website/get?id="+websiteID, authz, nil); got.Code != http.StatusOK {
		t.Fatalf("website get=%#v", got)
	}
	snapshot, err := store.LoadProxySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	business := proxy.New(snapshot)
	proxyResponse := httptest.NewRecorder()
	business.ServeHTTP(proxyResponse, httptest.NewRequest(http.MethodGet, "http://integration.example.invalid/simple", nil))
	if proxyResponse.Code != 218 || proxyResponse.Header().Get("X-Integration") != "present" || proxyResponse.Body.String() != "integration" {
		t.Fatalf("persisted simple response=%d headers=%v body=%q", proxyResponse.Code, proxyResponse.Header(), proxyResponse.Body.String())
	}
	if got := call(t, router, http.MethodDelete, "/api/website/delete?id="+websiteID, authz, nil); got.Code != http.StatusOK {
		t.Fatalf("website delete=%#v", got)
	}
	domain := call(t, router, http.MethodPost, "/api/web_domain/upsert", authz, map[string]any{"name": " INTEGRATION.EXAMPLE.INVALID.:443 ", "autoRequestCertificate": true, "isVerified": true, "freeCertMessage": "reused"})
	if domain.Code != http.StatusOK {
		t.Fatalf("domain=%#v", domain)
	}
	domainData := domain.Data.(map[string]any)
	if _, ok := domainData["id"].(string); !ok {
		t.Fatalf("domain id must be a string: %#v", domain.Data)
	}
	domainID := fmt.Sprint(domainData["id"])
	if domainID != fmt.Sprint(managedDomainID) || domainData["name"] != "integration.example.invalid" || domainData["autoRequestCertificate"] != true || domainData["isVerified"] != true || domainData["freeCertMessage"] != "reused" {
		t.Fatalf("domain upsert did not reuse and update managed row: %#v, original id=%d", domainData, managedDomainID)
	}
	if got := call(t, router, http.MethodPost, "/api/web_domain/search", authz, map[string]any{}); got.Code != http.StatusOK {
		t.Fatalf("domain search=%#v", got)
	}
	if got := call(t, router, http.MethodGet, "/api/web_domain/get?id="+domainID, authz, nil); got.Code != http.StatusOK {
		t.Fatalf("domain get=%#v", got)
	}
	if got := call(t, router, http.MethodDelete, "/api/web_domain/delete?id="+domainID, authz, nil); got.Code != http.StatusOK {
		t.Fatalf("domain delete=%#v", got)
	}
	largeDomainID := "9007199254740993"
	activeDomain := "active-cert.example.invalid"
	inactiveDomain := "inactive-cert.example.invalid"
	expiredDomain := "expired-cert.example.invalid"
	missingDomain := "missing-cert.example.invalid"
	for offset, name := range []string{activeDomain, inactiveDomain, expiredDomain, missingDomain} {
		if _, err := pool.Exec(ctx, "INSERT INTO web_domain(id,name,is_use_free_cert,is_verified,free_cert_message) VALUES($1,$2,false,false,'')", int64(9007199254740993+offset), name); err != nil {
			t.Fatal(err)
		}
	}
	notBefore := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	notAfter := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := pool.Exec(ctx, "INSERT INTO gateway_certificate(fingerprint,fingerprint_sha256,dns_names,not_before,not_after,source,status,is_active,certificate_pem,private_key_pem) VALUES($1,$2,$3,$4,$5,'acme','active',true,$6,$6)", strings.Repeat("a", 64), strings.Repeat("a", 64), []string{activeDomain}, notBefore, notAfter, []byte("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO gateway_certificate(fingerprint,fingerprint_sha256,dns_names,not_before,not_after,source,status,is_active,certificate_pem,private_key_pem) VALUES($1,$2,$3,$4,$5,'acme','active',false,$6,$6)", strings.Repeat("b", 64), strings.Repeat("b", 64), []string{inactiveDomain}, notBefore, notAfter, []byte("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO gateway_certificate(fingerprint,fingerprint_sha256,dns_names,not_before,not_after,source,status,is_active,certificate_pem,private_key_pem) VALUES($1,$2,$3,$4,$5,'acme','active',true,$6,$6)", strings.Repeat("c", 64), strings.Repeat("c", 64), []string{expiredDomain}, notBefore.Add(-2*time.Hour), notBefore, []byte("test")); err != nil {
		t.Fatal(err)
	}
	legacyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer legacyUpstream.Close()
	legacyIDs := map[string]int64{}
	for _, host := range []string{activeDomain, inactiveDomain, expiredDomain, missingDomain} {
		specifications := fmt.Sprintf(`{"defaultHost":%q,"defaultDestination":%q,"defaultDestinationHost":"","webProxy":"","authorizationPolicy":"","preserveOriginalHost":false,"allowInvalidTlsCertificate":false,"redirectHttpToHttps":false}`, host, legacyUpstream.URL)
		var id int64
		if err := pool.QueryRow(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses,create_time,update_time) VALUES($1,$2::jsonb,'[]'::jsonb,'[]'::jsonb,'{}'::jsonb,'[]'::jsonb,clock_timestamp(),clock_timestamp()) RETURNING id", "legacy-"+host, specifications).Scan(&id); err != nil {
			t.Fatal(err)
		}
		legacyIDs[host] = id
	}
	legacySearch := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{})
	if legacySearch.Code != http.StatusOK {
		t.Fatalf("legacy website search=%#v", legacySearch)
	}
	findWebsite := func(name string) map[string]any {
		for _, value := range pageRecords(legacySearch.Data) {
			record := value.(map[string]any)
			if record["name"] == name {
				return record
			}
		}
		t.Fatalf("website %q is missing from %#v", name, legacySearch.Data)
		return nil
	}
	for _, host := range []string{activeDomain, inactiveDomain, expiredDomain, missingDomain} {
		record := findWebsite("legacy-" + host)
		if record["defaultHost"] != host || record["defaultDestination"] != legacyUpstream.URL || record["hasCertificate"] != (host == activeDomain) {
			t.Fatalf("legacy website record for %q=%#v", host, record)
		}
		if _, exists := record["DefaultHost"]; exists {
			t.Fatalf("non-contract field leaked into API response: %#v", record)
		}
	}
	legacyGet := call(t, router, http.MethodGet, "/api/website/get?id="+strconv.FormatInt(legacyIDs[activeDomain], 10), authz, nil)
	if legacyGet.Code != http.StatusOK || legacyGet.Data.(map[string]any)["defaultHost"] != activeDomain || legacyGet.Data.(map[string]any)["hasCertificate"] != true {
		t.Fatalf("legacy website get=%#v", legacyGet)
	}
	legacyEditorSpec := `{"defaultHost":"editor-legacy.example.invalid","defaultDestination":"https://legacy-origin.example.invalid"}`
	var legacyEditorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO website(name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses,create_time,update_time)
VALUES($1,$2::jsonb,$3::jsonb,$4::jsonb,$5::jsonb,$6::jsonb,clock_timestamp(),clock_timestamp()) RETURNING id`,
		"legacy-editor-site",
		legacyEditorSpec,
		`["app.editor-legacy.example.invalid"]`,
		`[{"name":"legacy-route","pathPattern":"/api","destination":"http://127.0.0.1:5000","isEnabled":true}]`,
		`{}`,
		`[{"path":"/healthz","statusCode":218,"contentType":"text/plain","responseContent":"healthy"}]`,
	).Scan(&legacyEditorID); err != nil {
		t.Fatal(err)
	}
	legacyEditorGet := call(t, router, http.MethodGet, "/api/website/get?id="+strconv.FormatInt(legacyEditorID, 10), authz, nil)
	legacyEditorData := legacyEditorGet.Data.(map[string]any)
	if legacyEditorGet.Code != http.StatusOK ||
		legacyEditorData["defaultHost"] != "editor-legacy.example.invalid" ||
		legacyEditorData["hosts"].([]any)[0] != "app.editor-legacy.example.invalid" ||
		legacyEditorData["proxyRules"].([]any)[0].(map[string]any)["pathPattern"] != "/api" ||
		len(legacyEditorData["proxyTransforms"].([]any)) != 0 ||
		legacyEditorData["simpleResponses"].([]any)[0].(map[string]any)["statusCode"] != float64(218) {
		t.Fatalf("legacy website editor response=%#v", legacyEditorGet)
	}
	var nullSpecificationsID int64
	if err := pool.QueryRow(ctx, `INSERT INTO website(name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses,create_time,update_time)
VALUES($1,NULL,$2::jsonb,$3::jsonb,$4::jsonb,$5::jsonb,clock_timestamp(),clock_timestamp()) RETURNING id`,
		"null-specifications-site",
		`["null-spec.example.invalid"]`,
		`[{"pathPattern":"/api","destination":"https://origin.example.invalid","isEnabled":true,"webProxy":"http://proxy.example.invalid:8080","preserveOriginalHost":true,"allowInvalidTlsCertificate":true}]`,
		`{}`,
		`[{"path":"/healthz","statusCode":218,"contentType":"text/plain","responseContent":"healthy","extraHeaders":[{"key":"X-Null-Spec","value":"present"}]}]`,
	).Scan(&nullSpecificationsID); err != nil {
		t.Fatal(err)
	}
	nullSpecificationsGet := call(t, router, http.MethodGet, "/api/website/get?id="+strconv.FormatInt(nullSpecificationsID, 10), authz, nil)
	nullSpecificationsData := nullSpecificationsGet.Data.(map[string]any)
	if nullSpecificationsGet.Code != http.StatusOK ||
		nullSpecificationsData["hosts"].([]any)[0] != "null-spec.example.invalid" ||
		nullSpecificationsData["proxyRules"].([]any)[0].(map[string]any)["webProxy"] != "http://proxy.example.invalid:8080" ||
		len(nullSpecificationsData["proxyTransforms"].([]any)) != 0 ||
		nullSpecificationsData["simpleResponses"].([]any)[0].(map[string]any)["extraHeaders"].([]any)[0].(map[string]any)["key"] != "X-Null-Spec" {
		t.Fatalf("null specifications website editor response=%#v", nullSpecificationsGet)
	}
	legacySnapshot, err := store.LoadProxySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	legacyProxy := proxy.New(legacySnapshot)
	legacyResponse := httptest.NewRecorder()
	legacyProxy.ServeHTTP(legacyResponse, httptest.NewRequest(http.MethodGet, "http://"+activeDomain+"/", nil))
	if legacyResponse.Code != http.StatusNoContent {
		t.Fatalf("legacy website proxy response=%d body=%q", legacyResponse.Code, legacyResponse.Body.String())
	}
	filterSite := call(t, router, http.MethodPost, "/api/website/upsert", authz, map[string]any{
		"name":               "filter-site",
		"defaultHost":        "primary-filter.example.invalid",
		"hosts":              []string{"attached-filter.example.invalid"},
		"defaultDestination": "https://filter-upstream.example.invalid",
	})
	if filterSite.Code != http.StatusOK {
		t.Fatalf("filter website=%#v", filterSite)
	}
	filterSiteID := fmt.Sprint(filterSite.Data.(map[string]any)["id"])
	containsWebsite := func(records []any, id string) bool {
		for _, value := range records {
			if fmt.Sprint(value.(map[string]any)["id"]) == id {
				return true
			}
		}
		return false
	}
	if got := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{"domain": "ATTACHED-filter"}); got.Code != http.StatusOK || !containsWebsite(pageRecords(got.Data), filterSiteID) {
		t.Fatalf("attached-domain filter=%#v", got)
	}
	if got := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{"upstream": "FILTER-upstream"}); got.Code != http.StatusOK || !containsWebsite(pageRecords(got.Data), filterSiteID) {
		t.Fatalf("upstream filter=%#v", got)
	}
	// 新版编辑器只写主机绑定、转发规则和上游池，不再填 defaultHost / hosts / defaultDestination。
	bindingSite := call(t, router, http.MethodPost, "/api/website/upsert", authz, map[string]any{
		"name": "binding-only-site",
		"hostBindings": []map[string]any{
			{"hostname": "bound-only.example.invalid", "enabled": true, "role": "primary", "accessMode": "proxy"},
		},
		"upstreamPools": []map[string]any{
			{"id": "pool-a", "name": "pool", "strategy": "weightedRoundRobin", "endpoints": []map[string]any{
				{"address": "http://pool-endpoint.example.invalid:8080", "weight": 1, "enabled": true},
			}},
		},
		"proxyRules": []map[string]any{
			{"name": "direct", "isEnabled": true, "pathPattern": "/direct/", "destination": "http://rule-destination.example.invalid"},
			{"name": "pooled", "isEnabled": true, "isFallback": true, "upstreamPoolId": "pool-a"},
		},
	})
	if bindingSite.Code != http.StatusOK {
		t.Fatalf("binding website=%#v", bindingSite)
	}
	bindingSiteID := fmt.Sprint(bindingSite.Data.(map[string]any)["id"])
	for name, query := range map[string]map[string]any{
		"host binding":     {"domain": "BOUND-only"},
		"rule destination": {"upstream": "RULE-destination"},
		"pool endpoint":    {"upstream": "pool-ENDPOINT"},
	} {
		if got := call(t, router, http.MethodPost, "/api/website/search", authz, query); got.Code != http.StatusOK || !containsWebsite(pageRecords(got.Data), bindingSiteID) {
			t.Fatalf("%s filter=%#v", name, got)
		}
	}
	if got := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{"domain": "bound-only", "upstream": "no-such-upstream"}); got.Code != http.StatusOK || containsWebsite(pageRecords(got.Data), bindingSiteID) {
		t.Fatalf("combined filters must both match: %#v", got)
	}
	if got := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{"hasCertificate": true}); got.Code != http.StatusOK || !containsWebsite(pageRecords(got.Data), strconv.FormatInt(legacyIDs[activeDomain], 10)) || containsWebsite(pageRecords(got.Data), filterSiteID) {
		t.Fatalf("certificate-present filter=%#v", got)
	}
	if got := call(t, router, http.MethodPost, "/api/website/search", authz, map[string]any{"hasCertificate": false}); got.Code != http.StatusOK || !containsWebsite(pageRecords(got.Data), filterSiteID) || containsWebsite(pageRecords(got.Data), strconv.FormatInt(legacyIDs[activeDomain], 10)) {
		t.Fatalf("certificate-absent filter=%#v", got)
	}
	batchSite := call(t, router, http.MethodPost, "/api/website/upsert", authz, map[string]any{"name": "batch-delete-site"})
	if batchSite.Code != http.StatusOK {
		t.Fatalf("batch website=%#v", batchSite)
	}
	batchSiteID := fmt.Sprint(batchSite.Data.(map[string]any)["id"])
	batchDelete := call(t, router, http.MethodPost, "/api/website/delete_batch", authz, map[string]any{"ids": []string{batchSiteID, "999999"}})
	if batchDelete.Code != http.StatusOK {
		t.Fatalf("batch delete=%#v", batchDelete)
	}
	batchResult := batchDelete.Data.(map[string]any)
	if !containsWebsiteID(batchResult["deletedIds"].([]any), batchSiteID) || !containsWebsiteID(batchResult["notFoundIds"].([]any), "999999") {
		t.Fatalf("batch delete result=%#v", batchResult)
	}
	var deleteEvents int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gateway_config_event WHERE resource_type='website' AND resource_id=$1 AND operation='delete'", batchSiteID).Scan(&deleteEvents); err != nil || deleteEvents != 1 {
		t.Fatalf("batch delete config event count=%d err=%v", deleteEvents, err)
	}
	search := call(t, router, http.MethodPost, "/api/web_domain/search", authz, map[string]any{})
	if search.Code != http.StatusOK {
		t.Fatalf("large domain search=%#v", search)
	}
	records := pageRecords(search.Data)
	findDomain := func(name string) map[string]any {
		for _, value := range records {
			record := value.(map[string]any)
			if record["name"] == name {
				return record
			}
		}
		t.Fatalf("domain %q is missing from %#v", name, records)
		return nil
	}
	activeRecord := findDomain(activeDomain)
	if activeRecord["id"] != largeDomainID {
		t.Fatalf("large domain id=%#v want=%q", activeRecord["id"], largeDomainID)
	}
	activeCert := activeRecord["certSpec"].(map[string]any)
	if activeCert["hasCertificate"] != true || activeCert["thumbprint"] != strings.Repeat("a", 64) || activeCert["notBefore"] != float64(notBefore.Unix()) || activeCert["notAfter"] != float64(notAfter.Unix()) {
		t.Fatalf("active certificate spec=%#v", activeCert)
	}
	for _, name := range []string{inactiveDomain, expiredDomain, missingDomain} {
		cert := findDomain(name)["certSpec"].(map[string]any)
		if cert["hasCertificate"] != false || cert["thumbprint"] != nil {
			t.Fatalf("inactive certificate spec for %q=%#v", name, cert)
		}
	}
	if got := call(t, router, http.MethodGet, "/api/web_domain/get?id="+largeDomainID, authz, nil); got.Code != http.StatusOK || got.Data.(map[string]any)["id"] != largeDomainID {
		t.Fatalf("large domain get=%#v", got)
	}
	if got := call(t, router, http.MethodPost, "/api/web_domain/upsert", authz, map[string]any{"id": largeDomainID, "name": activeDomain, "autoRequestCertificate": true, "isVerified": true}); got.Code != http.StatusOK || got.Data.(map[string]any)["id"] != largeDomainID {
		t.Fatalf("large domain upsert=%#v", got)
	}
	if got := call(t, router, http.MethodDelete, "/api/web_domain/delete?id="+largeDomainID, authz, nil); got.Code != http.StatusOK {
		t.Fatalf("large domain delete=%#v", got)
	}
	nodeID := uuid.NewString()
	nodeInput := management.NodeRegistrationRequest{NodeID: nodeID, MachineFingerprint: strings.Repeat("a", 64), DisplayName: "integration", SoftwareVersion: "test", GitSHA: "test"}
	if got := call(t, router, http.MethodPost, "/api/node/register", nil, nodeInput); got.Code != http.StatusOK {
		_, err := store.RegisterNode(ctx, nodeInput)
		t.Fatalf("register=%#v storeErr=%v", got, err)
	}
	if got := call(t, router, http.MethodPost, "/api/gateway_node/search", authz, map[string]any{"pageIndex": 1, "pageSize": 50, "status": "active"}); got.Code != http.StatusOK {
		t.Fatalf("gateway node search=%#v", got)
	} else if records := got.Data.(map[string]any)["records"].([]any); len(records) != 1 {
		t.Fatalf("gateway node records=%#v", got.Data)
	}
	if got := call(t, router, http.MethodGet, "/api/gateway_node/get?id="+nodeID, authz, nil); got.Code != http.StatusOK {
		t.Fatalf("gateway node get=%#v", got)
	} else if node := got.Data.(map[string]any); node["nodeId"] != nodeID || node["status"] != "active" || node["reloadGeneration"] != "0" {
		t.Fatalf("gateway node data=%#v", node)
	}
	if got := call(t, router, http.MethodPost, "/api/gateway_node/reload", authz, map[string]any{"nodeId": nodeID}); got.Code != http.StatusOK {
		t.Fatalf("gateway node reload=%#v", got)
	} else if reload := got.Data.(map[string]any); reload["reloadGeneration"] != "1" {
		t.Fatalf("gateway node reload data=%#v", reload)
	}
	if got := call(t, router, http.MethodPost, "/api/gateway_node/disable", authz, map[string]any{"nodeId": nodeID, "reason": "integration"}); got.Code != http.StatusOK {
		t.Fatalf("gateway node disable=%#v", got)
	} else if node := got.Data.(map[string]any); node["status"] != "draining" {
		t.Fatalf("gateway node disable data=%#v", node)
	}
	if err := store.CompleteNodeDrain(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if got := call(t, router, http.MethodGet, "/api/gateway_node/get?id="+nodeID, authz, nil); got.Code != http.StatusOK || got.Data.(map[string]any)["status"] != "disabled" {
		t.Fatalf("gateway node final state=%#v", got)
	}
	store.SetNodeHeartbeatStaleAfter(time.Nanosecond)
	if got := call(t, router, http.MethodPost, "/api/gateway_node/disable", authz, map[string]any{"nodeId": nodeID, "reason": "stale integration"}); got.Code != http.StatusOK || got.Data.(map[string]any)["status"] != "disabled" {
		t.Fatalf("stale node must disable without drain=%#v", got)
	}
	upstreamStatus := http.StatusBadGateway
	requestAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	const requestID int64 = 79626516022960137
	store.SetRequestIDGenerator(func() (int64, error) { return requestID, nil })
	if err := store.Flush(ctx, []telemetry.Event{{Host: "integration.example.invalid", Method: http.MethodGet, Path: "/", Status: http.StatusBadGateway, HistoryStatus: 604, StatusSource: "YaginxProxy", ErrorCategory: "UpstreamRequestFailure", At: requestAt, UserAgent: "integration-agent", RequestHeaders: map[string][]string{"X-Request": {"one"}}, ResponseHeaders: map[string][]string{"X-Response": {"two"}}, RequestBodyBase64: "cmVxdWVzdA==", ResponseBodyBase64: "cmVzcG9uc2U=", RequestContentType: "application/json", ResponseContentType: "application/json", RequestBodyTruncated: true, ResponseBodyTruncated: true, UpstreamStatus: &upstreamStatus}}); err != nil {
		t.Fatal(err)
	}
	// The analytics read models that replaced the host_traffic stack carry their
	// own durable coverage in repository/observability_integration_test.go; what
	// this flush has to prove here is that the request history it writes is
	// readable through the management surface.
	pagedHistory, err := store.SearchRequestHistory(ctx, management.RequestHistorySearchQuery{PageRequest: management.PageRequest{PageIndex: 1, PageSize: 50}})
	if err != nil || len(pagedHistory.Records) != 1 {
		t.Fatalf("paged history=%#v err=%v", pagedHistory, err)
	} else if id := pagedHistory.Records[0].ID; id != "79626516022960137" {
		t.Fatalf("paged history id=%#v must preserve the Snowflake ID as a string", pagedHistory)
	}
	if history, err := store.SearchRequestHistory(ctx, management.RequestHistorySearchQuery{PageRequest: management.PageRequest{PageIndex: 1, PageSize: 50}}); err != nil || len(history.Records) != 1 {
		t.Fatalf("history=%#v err=%v", history, err)
	} else if history.Records[0].ID != "79626516022960137" || history.Records[0].StatusCode != 604 || history.Records[0].StatusSource != "YaginxProxy" || history.Records[0].ErrorCategory != "UpstreamRequestFailure" || history.Records[0].UpstreamStatusCode == nil || *history.Records[0].UpstreamStatusCode != http.StatusBadGateway {
		t.Fatalf("history classification=%#v", history.Records[0])
	} else if detail, err := store.GetRequestHistory(ctx, history.Records[0].ID); err != nil || detail.ID != "79626516022960137" || detail.StatusCode != 604 || detail.StatusSource != "YaginxProxy" || detail.ErrorCategory != "UpstreamRequestFailure" || detail.RequestBodyBase64 != "cmVxdWVzdA==" || detail.ResponseBodyBase64 != "cmVzcG9uc2U=" || !detail.RequestBodyTruncated || !detail.ResponseBodyTruncated || detail.RequestAt != requestAt.Unix() {
		t.Fatalf("history detail=%#v err=%v", detail, err)
	}
}

func isEmptyObject(value any) bool {
	object, ok := value.(map[string]any)
	return ok && len(object) == 0
}

type envelope struct {
	Code   int `json:"code"`
	Data   any `json:"data"`
	ErrMsg any `json:"errMsg"`
}

func call(t *testing.T, handler http.Handler, method, path string, headers map[string]string, body any) envelope {
	t.Helper()
	var input *bytes.Reader
	if body == nil {
		input = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, input)
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result envelope
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("path=%s status=%d body=%s err=%v", path, response.Code, response.Body.String(), err)
	}
	return result
}

func containsWebsiteID(values []any, id string) bool {
	for _, value := range values {
		if fmt.Sprint(value) == id {
			return true
		}
	}
	return false
}

func pageRecords(data any) []any {
	return data.(map[string]any)["records"].([]any)
}

func uniqueSchema(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return "yaginx_goal_it" + hex.EncodeToString(raw)
}

type fakeSMTP struct {
	listener net.Listener
	mu       sync.Mutex
	body     string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &fakeSMTP{listener: listener}
	go server.serve()
	return server
}
func (s *fakeSMTP) Port() int { return s.listener.Addr().(*net.TCPAddr).Port }
func (s *fakeSMTP) Close()    { _ = s.listener.Close() }
func (s *fakeSMTP) serve() {
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.session(connection)
	}
}
func (s *fakeSMTP) session(connection net.Conn) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	_, _ = fmt.Fprint(connection, "220 integration SMTP\r\n")
	inData := false
	var body strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				s.mu.Lock()
				s.body = body.String()
				s.mu.Unlock()
				inData = false
				_, _ = fmt.Fprint(connection, "250 queued\r\n")
			} else {
				body.WriteString(line)
				body.WriteByte('\n')
			}
			continue
		}
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "EHLO"), strings.HasPrefix(strings.ToUpper(line), "HELO"):
			_, _ = fmt.Fprint(connection, "250-localhost\r\n250 OK\r\n")
		case strings.EqualFold(line, "DATA"):
			inData = true
			_, _ = fmt.Fprint(connection, "354 end with .\r\n")
		case strings.EqualFold(line, "QUIT"):
			_, _ = fmt.Fprint(connection, "221 bye\r\n")
			return
		default:
			_, _ = fmt.Fprint(connection, "250 OK\r\n")
		}
	}
}
func (s *fakeSMTP) Code(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		body := s.body
		s.mu.Unlock()
		if code := regexp.MustCompile(`\b[0-9]{6}\b`).FindString(body); code != "" {
			return code
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("SMTP did not receive recovery code")
	return ""
}
