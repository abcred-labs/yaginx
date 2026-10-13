package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yaginx-gateway/internal/auth"
	"yaginx-gateway/internal/contract"
	"yaginx-gateway/internal/management"
)

// TestAccountFixturesReplayThroughGoRouter converts captured v2 exchanges
// into executable handler checks. Repository-heavy list/report operations
// have their own fixture lanes; this lane owns account/session and recovery.
func TestAccountFixturesReplayThroughGoRouter(t *testing.T) {
	fixtures, err := contract.LoadFixtures(filepath.Join("..", "..", "..", "tests", "contract", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"accountLogin": true, "accountProfileGet": true, "accountProfileUpdate": true, "accountRefreshToken": true,
		"accountRegister": true, "passwordRecoveryStart": true, "passwordRecoveryVerifyEmail": true, "passwordRecoveryReset": true,
		"websiteUpsert": true, "websiteRuleRefresh": true, "websiteDelete": true, "websiteSearchGet": true, "websiteSearchPost": true, "websiteGet": true, "websiteGetByName": true,
		"webDomainUpsert": true, "webDomainDelete": true, "webDomainSearchGet": true, "webDomainSearchPost": true, "webDomainGet": true, "webDomainGetByName": true,
		"smtpGet": true, "smtpUpsert": true, "smtpTestSend": true,
		"requestHistorySearch": true, "requestHistoryGet": true,
	}
	keys := auth.Keys{Current: []byte("01234567890123456789012345678901"), Issuer: "Yaginx Proxy Server", Audience: "Yaingx Clients", Session: 10 * time.Minute}
	for _, fixture := range fixtures {
		fixture := fixture
		if !wanted[fixture.OperationID] {
			continue
		}
		t.Run(fixture.OperationID+"/"+fixture.Scenario, func(t *testing.T) {
			expected := decodeFixtureResponse(t, fixture)
			store := fixtureStore{value: expected["data"]}
			if fixture.Scenario == "business_error" || fixture.Scenario == "parameter_error" {
				store.err = errors.New("fixture failure")
				expected["errMsg"] = "fixture failure"
			}
			router := New(Options{Store: store, Keys: keys})
			request := httptest.NewRequest(fixture.Request.Method, fixture.Request.Path, fixtureRequestBody(t, fixture.Request.Body))
			request.Header.Set("Content-Type", "application/json")
			if fixture.Request.Headers["Authorization"] != "" && fixture.Scenario != "auth" {
				token, err := keys.IssueIdentity("42", "fixture@example.invalid", "fixture@example.invalid", "", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Authorization", "Bearer "+token)
			}
			if fixture.OperationID == "accountRefreshToken" && fixture.Scenario == "success" {
				// accountRefreshToken authenticates via the yaginx-refresh
				// cookie, not a bearer token (see approved-deviations.json
				// v2-api-account-refresh-token-cookie-success); fixtureStore's
				// RotateAccountSession accepts any secret, mirroring how
				// Login accepts any password.
				request.AddCookie(&http.Cookie{Name: "yaginx-refresh", Value: "1.fixture-secret"})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != fixture.Response.Status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, fixture.Response.Status, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("content-type=%q", contentType)
			}
			actual := map[string]any{}
			if err := json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			assertGoldenValue(t, expected, actual, "response")
			if fixture.Scenario == "success" && (fixture.OperationID == "accountLogin" || fixture.OperationID == "accountRefreshToken") {
				cookies := response.Result().Cookies()
				var token, refresh *http.Cookie
				for _, cookie := range cookies {
					switch cookie.Name {
					case "yaginx-token":
						token = cookie
					case "yaginx-refresh":
						refresh = cookie
					}
				}
				if token == nil || !token.Secure || !token.HttpOnly || token.Path != "/" {
					t.Fatalf("legacy token cookie=%#v", token)
				}
				if refresh == nil || !refresh.Secure || !refresh.HttpOnly || refresh.Path != "/api/account" {
					t.Fatalf("refresh cookie=%#v", refresh)
				}
				if len(cookies) != 2 {
					t.Fatalf("expected exactly 2 auth cookies, got %#v", cookies)
				}
			}
		})
	}
}

func decodeFixtureResponse(t *testing.T, fixture contract.Fixture) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(fixture.Response.Body, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureRequestBody(t *testing.T, raw json.RawMessage) *bytes.Reader {
	t.Helper()
	if len(raw) == 0 {
		return bytes.NewReader(nil)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	replaceFixtureMarkers(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(encoded)
}

func replaceFixtureMarkers(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok && strings.HasPrefix(text, "${") {
				switch {
				case strings.Contains(strings.ToUpper(key), "EMAIL"):
					typed[key] = "fixture@example.invalid"
				case strings.Contains(strings.ToUpper(key), "PASSWORD"):
					typed[key] = "fixture-password"
				default:
					typed[key] = "fixture-value"
				}
				continue
			}
			replaceFixtureMarkers(item)
		}
	case []any:
		for _, item := range typed {
			replaceFixtureMarkers(item)
		}
	}
}

func assertGoldenValue(t *testing.T, expected, actual any, path string) {
	t.Helper()
	if marker, ok := expected.(string); ok && strings.HasPrefix(marker, "${") && strings.HasSuffix(marker, "}") {
		value, ok := actual.(string)
		if !ok || value == "" {
			t.Fatalf("%s expected dynamic marker %q, got %#v", path, marker, actual)
		}
		return
	}
	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok || len(want) != len(got) {
			t.Fatalf("%s map mismatch want=%#v got=%#v", path, want, actual)
		}
		for key, child := range want {
			value, exists := got[key]
			if !exists {
				t.Fatalf("%s.%s is missing", path, key)
			}
			assertGoldenValue(t, child, value, path+"."+key)
		}
	case []any:
		got, ok := actual.([]any)
		if !ok || len(want) != len(got) {
			t.Fatalf("%s list mismatch want=%#v got=%#v", path, want, actual)
		}
		for index := range want {
			assertGoldenValue(t, want[index], got[index], fmt.Sprintf("%s[%d]", path, index))
		}
	default:
		if fmt.Sprint(expected) != fmt.Sprint(actual) {
			t.Fatalf("%s mismatch want=%#v got=%#v", path, expected, actual)
		}
	}
}

type fixtureStore struct {
	value                any
	err                  error
	requestHistorySearch func(context.Context, management.RequestHistorySearchQuery) (management.RequestHistoryPage, error)
	observabilityDash    func(context.Context, management.AnalyticsOverviewQuery) (management.AnalyticsOverview, error)
	resourceCatalog      func(context.Context, management.ResourceCatalogQuery) (management.ResourceCatalog, error)
	domainResourceView   func(context.Context, management.DomainResourceViewQuery) (management.DomainResourceView, error)
	websiteResourceView  func(context.Context, management.WebsiteResourceViewQuery) (management.WebsiteResourceView, error)
	databasePerformance  func(context.Context) (management.DatabasePerformance, error)
	websiteSearch        func(context.Context, management.WebsiteSearchFilter) ([]management.Website, error)
	websiteSearchPaged   func(context.Context, management.WebsiteSearchRequest) (management.Page[management.Website], error)
	deleteWebsite        func(context.Context, string) error
	deleteWebsites       func(context.Context, []int64) (management.WebsiteBatchDeleteResult, error)
}

func (s fixtureStore) Login(context.Context, string, string) (management.Account, error) {
	return management.Account{ID: 42, Email: "fixture@example.invalid"}, s.err
}
func (s fixtureStore) RegisterAccount(context.Context, string, string) error { return s.err }
func (s fixtureStore) StartPasswordRecovery(context.Context, string) (management.PasswordRecoveryStartResponse, error) {
	return fixtureValue[management.PasswordRecoveryStartResponse](s.value, s.err)
}
func (s fixtureStore) VerifyPasswordRecovery(context.Context, string, string) error { return s.err }
func (s fixtureStore) ResetPasswordRecovery(context.Context, string, string, string, string) error {
	return s.err
}
func (s fixtureStore) GetProfile(context.Context, string) (management.Profile, error) {
	return fixtureValue[management.Profile](s.value, s.err)
}
func (s fixtureStore) UpdateProfile(context.Context, string, management.ProfileUpdateRequest) error {
	return s.err
}
func (s fixtureStore) CreateAccountSession(context.Context, int64, []byte, bool, string, string, time.Duration, time.Duration) (int64, error) {
	return 1, s.err
}
func (s fixtureStore) RotateAccountSession(context.Context, int64, []byte, []byte, time.Duration, time.Duration) (int64, string, bool, error) {
	return 42, "fixture@example.invalid", false, s.err
}
func (s fixtureStore) RevokeAccountSessionByToken(context.Context, int64, []byte) error { return s.err }
func (s fixtureStore) RevokeAccountSession(context.Context, int64, int64) error         { return s.err }
func (s fixtureStore) RevokeOtherAccountSessions(context.Context, int64, int64) error   { return s.err }
func (s fixtureStore) ListAccountSessions(context.Context, int64) ([]management.SessionSummary, error) {
	return fixtureValue[[]management.SessionSummary](s.value, s.err)
}
func (s fixtureStore) UpsertWebsite(context.Context, management.WebsiteUpsertRequest) (management.Website, error) {
	return fixtureValue[management.Website](s.value, s.err)
}
func (s fixtureStore) ListWebsites(context.Context) ([]management.Website, error) {
	return fixtureValue[[]management.Website](s.value, s.err)
}
func (s fixtureStore) SearchWebsites(ctx context.Context, input management.WebsiteSearchRequest) (management.Page[management.Website], error) {
	if s.websiteSearchPaged != nil {
		return s.websiteSearchPaged(ctx, input)
	}
	if s.websiteSearch != nil {
		records, err := s.websiteSearch(ctx, input.WebsiteSearchFilter)
		return management.Page[management.Website]{Records: records, Paging: management.PageCursor{PageIndex: input.PageIndex, PageSize: input.PageSize, Total: int64(len(records))}}, err
	}
	return fixturePage[management.Website](s.value, input.PageRequest, s.err)
}
func (s fixtureStore) GetWebsite(context.Context, string) (management.Website, error) {
	if s.value == nil || s.err != nil {
		return management.Website{}, func() error {
			if s.err != nil {
				return s.err
			}
			return management.ErrNotFound
		}()
	}
	return fixtureValue[management.Website](s.value, s.err)
}
func (s fixtureStore) DeleteWebsite(ctx context.Context, id string) error {
	if s.deleteWebsite != nil {
		return s.deleteWebsite(ctx, id)
	}
	return s.err
}
func (s fixtureStore) DeleteWebsites(ctx context.Context, ids []int64) (management.WebsiteBatchDeleteResult, error) {
	if s.deleteWebsites != nil {
		return s.deleteWebsites(ctx, ids)
	}
	return management.WebsiteBatchDeleteResult{}, s.err
}
func (s fixtureStore) UpsertDomain(context.Context, management.DomainUpsertRequest) (management.Domain, error) {
	return fixtureValue[management.Domain](s.value, s.err)
}
func (s fixtureStore) ListDomains(context.Context) ([]management.DomainListItem, error) {
	return fixtureValue[[]management.DomainListItem](s.value, s.err)
}
func (s fixtureStore) SearchDomains(_ context.Context, input management.WebDomainSearchRequest) (management.Page[management.DomainListItem], error) {
	return fixturePage[management.DomainListItem](s.value, input.PageRequest, s.err)
}
func (s fixtureStore) GetDomain(context.Context, string) (management.DomainListItem, error) {
	if s.value == nil || s.err != nil {
		return management.DomainListItem{}, func() error {
			if s.err != nil {
				return s.err
			}
			return management.ErrNotFound
		}()
	}
	return fixtureValue[management.DomainListItem](s.value, s.err)
}
func (s fixtureStore) DeleteDomain(context.Context, string) error { return s.err }
func (s fixtureStore) GetSMTP(context.Context) (*management.SMTPSettings, error) {
	if s.value == nil || s.err != nil {
		return nil, s.err
	}
	value, err := fixtureValue[management.SMTPSettings](s.value, nil)
	return &value, err
}
func (s fixtureStore) SetSMTP(context.Context, management.SMTPSettings) error { return s.err }
func (s fixtureStore) ListAPICredentials(context.Context) ([]management.APICredential, error) {
	return fixtureValue[[]management.APICredential](s.value, s.err)
}
func (s fixtureStore) CreateAPICredential(context.Context, management.APICredentialCreateRequest) (management.APICredentialSecret, error) {
	return fixtureValue[management.APICredentialSecret](s.value, s.err)
}
func (s fixtureStore) ResetAPICredential(context.Context, string) (management.APICredentialSecret, error) {
	return fixtureValue[management.APICredentialSecret](s.value, s.err)
}
func (s fixtureStore) RevokeAPICredential(context.Context, string) (management.APICredential, error) {
	return fixtureValue[management.APICredential](s.value, s.err)
}
func (s fixtureStore) ValidateAPIToken(context.Context, string) bool { return false }
func (s fixtureStore) TestSMTP(context.Context, string) error        { return s.err }
func (s fixtureStore) GetRuntimeSetting(context.Context) (*management.RuntimeSetting, error) {
	if s.value == nil || s.err != nil {
		return nil, s.err
	}
	value, err := fixtureValue[management.RuntimeSetting](s.value, nil)
	return &value, err
}
func (s fixtureStore) SetRuntimeSetting(context.Context, management.RuntimeSettingUpsertRequest) (*management.RuntimeSetting, error) {
	if s.value == nil || s.err != nil {
		return nil, s.err
	}
	value, err := fixtureValue[management.RuntimeSetting](s.value, nil)
	return &value, err
}
func (s fixtureStore) RuntimeSettingStatus(context.Context) (management.RuntimeSettingStatus, error) {
	return fixtureValue[management.RuntimeSettingStatus](s.value, s.err)
}
func (s fixtureStore) RegisterNode(context.Context, management.NodeRegistrationRequest) (management.NodeSummary, error) {
	return fixtureValue[management.NodeSummary](s.value, s.err)
}

func (s fixtureStore) HeartbeatNode(context.Context, management.NodeHeartbeatRequest) (management.NodeSummary, error) {
	return fixtureValue[management.NodeSummary](s.value, s.err)
}
func (s fixtureStore) ListNodes(context.Context) ([]management.NodeSummary, error) {
	return fixtureValue[[]management.NodeSummary](s.value, s.err)
}
func (s fixtureStore) SearchGatewayNodes(context.Context, management.GatewayNodeSearchRequest) (management.GatewayNodePage, error) {
	return fixtureValue[management.GatewayNodePage](s.value, s.err)
}
func (s fixtureStore) GetGatewayNode(context.Context, string) (*management.GatewayNode, error) {
	if s.value == nil || s.err != nil {
		return nil, s.err
	}
	value, err := fixtureValue[management.GatewayNode](s.value, nil)
	return &value, err
}

func (s fixtureStore) DeleteGatewayNode(context.Context, string) error { return nil }
func (s fixtureStore) DisableGatewayNode(context.Context, string, string) (management.GatewayNode, error) {
	return fixtureValue[management.GatewayNode](s.value, s.err)
}
func (s fixtureStore) ReloadGatewayNode(context.Context, string) (management.GatewayNodeReloadResult, error) {
	return fixtureValue[management.GatewayNodeReloadResult](s.value, s.err)
}
func (s fixtureStore) RestartGatewayNode(context.Context, string) (management.GatewayNodeRestartResult, error) {
	return fixtureValue[management.GatewayNodeRestartResult](s.value, s.err)
}
func (s fixtureStore) SearchRequestHistory(ctx context.Context, input management.RequestHistorySearchQuery) (management.RequestHistoryPage, error) {
	if s.requestHistorySearch != nil {
		return s.requestHistorySearch(ctx, input)
	}
	return fixtureValue[management.RequestHistoryPage](s.value, s.err)
}
func (s fixtureStore) GetRequestHistory(context.Context, string) (management.RequestHistoryDetail, error) {
	return fixtureValue[management.RequestHistoryDetail](s.value, s.err)
}
func (s fixtureStore) SearchIPBlacklist(context.Context, management.IPBlacklistSearchRequest) (management.Page[management.IPBlacklistRule], error) {
	return management.Page[management.IPBlacklistRule]{}, s.err
}
func (s fixtureStore) GetIPBlacklist(context.Context, string) (management.IPBlacklistRule, error) {
	return management.IPBlacklistRule{}, s.err
}
func (s fixtureStore) UpsertIPBlacklist(context.Context, management.IPBlacklistUpsertRequest) (management.IPBlacklistRule, error) {
	return management.IPBlacklistRule{}, s.err
}
func (s fixtureStore) RemoveIPBlacklist(context.Context, string) (management.IPBlacklistRule, error) {
	return management.IPBlacklistRule{}, s.err
}
func (s fixtureStore) LookupIP(context.Context, string) (management.IPProfile, error) {
	return management.IPProfile{}, s.err
}
func (s fixtureStore) DatabasePerformance(ctx context.Context) (management.DatabasePerformance, error) {
	if s.databasePerformance != nil {
		return s.databasePerformance(ctx)
	}
	return fixtureValue[management.DatabasePerformance](s.value, s.err)
}
func (s fixtureStore) GatewayNodeHealthSummary(context.Context, int64) (management.GatewayHealthSummary, error) {
	return fixtureValue[management.GatewayHealthSummary](s.value, s.err)
}
func (s fixtureStore) AnalyticsSummary(context.Context, management.AnalyticsQuery) (management.AnalyticsSummary, error) {
	return fixtureValue[management.AnalyticsSummary](s.value, s.err)
}
func (s fixtureStore) AnalyticsDomains(context.Context, management.AnalyticsQuery) (management.AnalyticsDomains, error) {
	return fixtureValue[management.AnalyticsDomains](s.value, s.err)
}
func (s fixtureStore) AnalyticsTimeseries(context.Context, management.AnalyticsQuery) (management.AnalyticsTimeseries, error) {
	return fixtureValue[management.AnalyticsTimeseries](s.value, s.err)
}
func (s fixtureStore) AnalyticsContributions(context.Context, management.AnalyticsQuery) (management.AnalyticsContributions, error) {
	return fixtureValue[management.AnalyticsContributions](s.value, s.err)
}
func (s fixtureStore) AnalyticsDetails(context.Context, management.AnalyticsPagedQuery) (management.AnalyticsDetails, error) {
	return fixtureValue[management.AnalyticsDetails](s.value, s.err)
}
func (s fixtureStore) AnalyticsOverview(ctx context.Context, query management.AnalyticsOverviewQuery) (management.AnalyticsOverview, error) {
	if s.observabilityDash != nil {
		return s.observabilityDash(ctx, query)
	}
	return fixtureValue[management.AnalyticsOverview](s.value, s.err)
}
func (s fixtureStore) ResourceCatalog(ctx context.Context, input management.ResourceCatalogQuery) (management.ResourceCatalog, error) {
	if s.resourceCatalog != nil {
		return s.resourceCatalog(ctx, input)
	}
	return fixtureValue[management.ResourceCatalog](s.value, s.err)
}
func (s fixtureStore) DomainResourceView(ctx context.Context, input management.DomainResourceViewQuery) (management.DomainResourceView, error) {
	if s.domainResourceView != nil {
		return s.domainResourceView(ctx, input)
	}
	return fixtureValue[management.DomainResourceView](s.value, s.err)
}
func (s fixtureStore) WebsiteResourceView(ctx context.Context, input management.WebsiteResourceViewQuery) (management.WebsiteResourceView, error) {
	if s.websiteResourceView != nil {
		return s.websiteResourceView(ctx, input)
	}
	return fixtureValue[management.WebsiteResourceView](s.value, s.err)
}

func fixtureValue[T any](value any, storeErr error) (T, error) {
	var result T
	if storeErr != nil {
		return result, storeErr
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return result, err
	}
	return result, nil
}

func fixturePage[T any](value any, request management.PageRequest, storeErr error) (management.Page[T], error) {
	var result management.Page[T]
	if storeErr != nil {
		return result, storeErr
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err == nil {
		return result, nil
	}
	var records []T
	if err := json.Unmarshal(encoded, &records); err != nil {
		return result, err
	}
	result.Records = records
	result.Paging = management.PageCursor{PageIndex: request.PageIndex, PageSize: request.PageSize, Total: int64(len(records))}
	return result, nil
}
