package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// The analytics operations have no prior baseline: they are the Go-native
// contract that replaced the host_traffic and resource_report stacks, so their
// request validation and response shape are pinned here instead.

type stubStore struct {
	overviewQuery management.AnalyticsOverviewQuery
	query         management.AnalyticsQuery
	pagedQuery    management.AnalyticsPagedQuery
	domainQuery   management.DomainResourceViewQuery
	websiteQuery  management.WebsiteResourceViewQuery
	err           error
}

func (s *stubStore) AnalyticsSummary(context.Context, management.AnalyticsQuery) (management.AnalyticsSummary, error) {
	return management.AnalyticsSummary{}, s.err
}
func (s *stubStore) AnalyticsDomains(_ context.Context, query management.AnalyticsQuery) (management.AnalyticsDomains, error) {
	s.query = query
	return management.AnalyticsDomains{Domains: []management.AnalyticsDomain{{Domain: "a.example.invalid"}}}, s.err
}
func (s *stubStore) AnalyticsTimeseries(_ context.Context, query management.AnalyticsQuery) (management.AnalyticsTimeseries, error) {
	s.query = query
	return management.AnalyticsTimeseries{Domain: query.Domain}, s.err
}
func (s *stubStore) AnalyticsContributions(_ context.Context, query management.AnalyticsQuery) (management.AnalyticsContributions, error) {
	s.query = query
	return management.AnalyticsContributions{}, s.err
}
func (s *stubStore) AnalyticsDetails(_ context.Context, query management.AnalyticsPagedQuery) (management.AnalyticsDetails, error) {
	s.pagedQuery = query
	return management.AnalyticsDetails{}, s.err
}
func (s *stubStore) AnalyticsOverview(_ context.Context, query management.AnalyticsOverviewQuery) (management.AnalyticsOverview, error) {
	s.overviewQuery = query
	out := management.AnalyticsOverview{}
	for _, section := range query.Sections {
		switch section {
		case "overview":
			out.Overview = &management.AnalyticsSummary{}
		case "domains":
			out.Domains = []management.AnalyticsDomain{{Domain: "a.example.invalid"}}
		case "timeseries":
			out.Points = []management.AnalyticsPoint{{BucketStartAt: 1}}
		}
	}
	return out, s.err
}
func (s *stubStore) ResourceCatalog(context.Context, management.ResourceCatalogQuery) (management.ResourceCatalog, error) {
	return management.ResourceCatalog{}, s.err
}
func (s *stubStore) DomainResourceView(_ context.Context, query management.DomainResourceViewQuery) (management.DomainResourceView, error) {
	s.domainQuery = query
	return management.DomainResourceView{Host: management.ResourceHost{Host: query.Host}}, s.err
}
func (s *stubStore) WebsiteResourceView(_ context.Context, query management.WebsiteResourceViewQuery) (management.WebsiteResourceView, error) {
	s.websiteQuery = query
	return management.WebsiteResourceView{Website: management.Website{ID: 42}}, s.err
}

func serve(t *testing.T, store Store, inFlight func() int64, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	router := chi.NewRouter()
	Handler{Store: store, BodyMax: management.DefaultBodyMax, ActiveProxyRequests: inFlight}.Routes(router)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	envelope := map[string]any{}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("response is not an envelope: %v body=%s", err, response.Body.String())
	}
	return response, envelope
}

func TestOverviewAnswersOnlyRequestedSections(t *testing.T) {
	store := &stubStore{}
	response, envelope := serve(t, store, func() int64 { return 7 }, http.MethodPost, "/analytics/overview",
		`{"from":1,"to":2,"domain":"a.example.invalid","sections":["domains","overview","domains"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := store.overviewQuery.Sections; len(got) != 2 || got[0] != "domains" || got[1] != "overview" {
		t.Fatalf("sections reached the store un-deduplicated: %#v", got)
	}
	if store.overviewQuery.From != 1 || store.overviewQuery.To != 2 || store.overviewQuery.Domain != "a.example.invalid" {
		t.Fatalf("window did not reach the store: %#v", store.overviewQuery)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data=%#v", envelope["data"])
	}
	if _, present := data["points"]; present {
		t.Fatalf("an unrequested section was returned: %#v", data)
	}
	overview, ok := data["overview"].(map[string]any)
	if !ok {
		t.Fatalf("overview section is missing: %#v", data)
	}
	// Only the proxy knows the live in-flight count, so the handler and not the
	// store has to supply it.
	if overview["currentInFlight"] != float64(7) {
		t.Fatalf("currentInFlight=%#v", overview["currentInFlight"])
	}
}

func TestOverviewRejectsUnknownSection(t *testing.T) {
	for name, body := range map[string]string{
		"unknown": `{"sections":["overview","nope"]}`,
		"empty":   `{"sections":[]}`,
		"absent":  `{}`,
		"invalid": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			response, envelope := serve(t, &stubStore{}, nil, http.MethodPost, "/analytics/overview", body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if envelope["errMsg"] != "invalid sections" {
				t.Fatalf("errMsg=%#v", envelope["errMsg"])
			}
		})
	}
}

func TestSharedQueryContractReachesEveryOperation(t *testing.T) {
	for _, path := range []string{"/analytics/domains", "/analytics/timeseries", "/analytics/contributions"} {
		t.Run(path, func(t *testing.T) {
			store := &stubStore{}
			response, _ := serve(t, store, nil, http.MethodPost, path, `{"from":10,"to":20,"domain":"b.example.invalid"}`)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			want := management.AnalyticsQuery{From: 10, To: 20, Domain: "b.example.invalid"}
			if !reflect.DeepEqual(store.query, want) {
				t.Fatalf("query=%#v want=%#v", store.query, want)
			}
			bad, _ := serve(t, &stubStore{}, nil, http.MethodPost, path, `{"from":"ten"}`)
			if bad.Code != http.StatusBadRequest {
				t.Fatalf("malformed body status=%d", bad.Code)
			}
		})
	}
}

func TestDetailsNormalizesPaging(t *testing.T) {
	store := &stubStore{}
	response, _ := serve(t, store, nil, http.MethodPost, "/analytics/details", `{"from":1,"to":2}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if store.pagedQuery.PageIndex < 1 || store.pagedQuery.PageSize < 1 {
		t.Fatalf("paging was not normalized: %#v", store.pagedQuery.PageRequest)
	}
}

func TestSubjectUnifiesDomainAndWebsite(t *testing.T) {
	store := &stubStore{}
	_, envelope := serve(t, store, nil, http.MethodGet, "/analytics/subject?subjectType=domain&key=a.example.invalid&from=5&to=9", "")
	data := envelope["data"].(map[string]any)
	if data["subjectType"] != "domain" || data["website"] != nil {
		t.Fatalf("domain subject=%#v", data)
	}
	if hosts, ok := data["hosts"].([]any); !ok || len(hosts) != 1 {
		t.Fatalf("domain subject carries its single host: %#v", data["hosts"])
	}
	if store.domainQuery.Host != "a.example.invalid" || store.domainQuery.From != 5 || store.domainQuery.To != 9 {
		t.Fatalf("domain query=%#v", store.domainQuery)
	}

	store = &stubStore{}
	_, envelope = serve(t, store, nil, http.MethodGet, "/analytics/subject?subjectType=website&key=42", "")
	data = envelope["data"].(map[string]any)
	if data["subjectType"] != "website" {
		t.Fatalf("website subject=%#v", data)
	}
	if _, ok := data["website"].(map[string]any); !ok {
		t.Fatalf("website record is missing: %#v", data)
	}
	if store.websiteQuery.ID != "42" {
		t.Fatalf("website query=%#v", store.websiteQuery)
	}
}

func TestSubjectRejectsBadKeyAndType(t *testing.T) {
	for name, target := range map[string]string{
		"unknown type": "/analytics/subject?subjectType=cluster&key=a",
		"absent type":  "/analytics/subject?key=a",
		"absent key":   "/analytics/subject?subjectType=domain",
		"non-id key":   "/analytics/subject?subjectType=website&key=abc",
		"bad window":   "/analytics/subject?subjectType=domain&key=a&from=soon",
	} {
		t.Run(name, func(t *testing.T) {
			response, _ := serve(t, &stubStore{}, nil, http.MethodGet, target, "")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
}

// A subject the store cannot find answers 200 with a null payload, which is how
// the console distinguishes "no such Host" from a failed request.
func TestSubjectMissingAnswersNullData(t *testing.T) {
	store := &stubStore{err: management.ErrNotFound}
	response, envelope := serve(t, store, nil, http.MethodGet, "/analytics/subject?subjectType=domain&key=gone.example.invalid", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if envelope["data"] != nil {
		t.Fatalf("data=%#v", envelope["data"])
	}
}

func TestStoreFailurePropagates(t *testing.T) {
	store := &stubStore{err: errors.New("store down")}
	response, envelope := serve(t, store, nil, http.MethodPost, "/analytics/domains", `{}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if envelope["errMsg"] != "store down" {
		t.Fatalf("errMsg=%#v", envelope["errMsg"])
	}
}

// Observe tolerates a nil hook, so a domain handler never has to guard it.
func TestObserveRecordsOutcome(t *testing.T) {
	observed := map[string]string{}
	router := chi.NewRouter()
	Handler{
		Store:   &stubStore{},
		BodyMax: management.DefaultBodyMax,
		Observe: func(operation, result string, _ time.Duration) { observed[operation] = result },
	}.Routes(router)
	for target, want := range map[string]string{
		"/analytics/domains": "success",
	} {
		request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{}`))
		router.ServeHTTP(httptest.NewRecorder(), request)
		if observed["domains"] != want {
			t.Fatalf("%s observed=%#v", target, observed)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/analytics/details", strings.NewReader(`{`))
	router.ServeHTTP(httptest.NewRecorder(), request)
	if observed["details"] != "failure" {
		t.Fatalf("observed=%#v", observed)
	}
}
