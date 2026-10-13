package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"
)

type contributionStoreStub struct{ values []Contribution }

func (s *contributionStoreStub) SaveTrafficContributions(_ context.Context, values []Contribution) error {
	s.values = values
	return nil
}

func TestAggregatorPersistsDurationAndOptionalUserAgentDimensions(t *testing.T) {
	store := &contributionStoreStub{}
	aggregator := NewAggregatorWithUserAgent(store, "node", true, 2)
	when := time.Date(2000, 7, 28, 3, 0, 0, 0, time.UTC)
	err := aggregator.Flush(context.Background(), []Event{
		{Host: "report.example.invalid", Status: 200, DurationMS: 50, At: when, UserAgent: "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/120.0 Safari/537.36"},
		{Host: "report.example.invalid", Status: 502, DurationMS: 700, At: when, UserAgent: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 2 {
		t.Fatalf("contributions=%d", len(store.values))
	}
	for _, value := range store.values {
		if len(value.StatusCounts) == 0 || len(value.ResponseModeMetrics) == 0 {
			t.Fatalf("mode metrics leaked into status counts: %#v", value)
		}
		if value.DurationBuckets["<=100"] != 1 || value.DurationBuckets[">500 & <=1000"] != 1 || value.BrowserCounts["Chrome"] != 1 || value.SpiderCounts["Googlebot"] != 1 {
			t.Fatalf("contribution=%#v", value)
		}
		if !value.IsFinal {
			t.Fatalf("final state=%t for period=%d", value.IsFinal, value.Period)
		}
	}
}

func TestAggregatorSeparatesResponseModeMetrics(t *testing.T) {
	store := &contributionStoreStub{}
	when := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := NewAggregator(store, "node").Flush(context.Background(), []Event{
		{Host: "example.invalid", Status: 200, DurationMS: 20, ResponseMode: "stream", TTFBMS: 5, TTFTMS: 8, At: when},
	}); err != nil {
		t.Fatal(err)
	}
	value := store.values[0]
	if value.StatusCounts["200"] != 1 || len(value.StatusCounts) != 1 {
		t.Fatalf("status counts=%#v", value.StatusCounts)
	}
	metrics := value.ResponseModeMetrics["stream"]
	if metrics["requests"] != 1 || metrics["ttfb_count"] != 1 || value.DurationBuckets["<=100"] != 1 {
		t.Fatalf("mode metrics=%#v duration=%#v", metrics, value.DurationBuckets)
	}
}

func TestAggregatorLeavesUserAgentDimensionsDisabled(t *testing.T) {
	store := &contributionStoreStub{}
	aggregator := NewAggregator(store, "node")
	if err := aggregator.Flush(context.Background(), []Event{{Host: "report.example.invalid", Status: 200, DurationMS: 1, At: time.Now(), UserAgent: "Googlebot"}}); err != nil {
		t.Fatal(err)
	}
	for _, value := range store.values {
		if len(value.SpiderCounts) != 0 || len(value.BrowserCounts) != 0 || len(value.OSCounts) != 0 {
			t.Fatalf("disabled UA counts=%#v", value)
		}
	}
}

func TestAggregatorAccountsForUnknownUserAgentsWhenParsingIsEnabled(t *testing.T) {
	store := &contributionStoreStub{}
	aggregator := NewAggregatorWithUserAgent(store, "node", true, 2)
	if err := aggregator.Flush(context.Background(), []Event{{Host: "report.example.invalid", Status: 200, DurationMS: 1, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	for _, value := range store.values {
		if value.BrowserCounts["Unknown"] != 1 || value.OSCounts["Unknown"] != 1 {
			t.Fatalf("unknown UA coverage=%#v", value)
		}
	}
}

func TestAggregatorCanDeferCurrentPeriodPersistence(t *testing.T) {
	store := &contributionStoreStub{}
	aggregator := NewAggregatorWithSettings(store, "11111111-1111-1111-1111-111111111111", false, 0, false)
	if err := aggregator.Flush(context.Background(), []Event{{Host: "deferred.example", At: time.Now().UTC(), Status: 200}}); err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 0 {
		t.Fatalf("current contributions persisted despite disabled update: %#v", store.values)
	}
}

func TestAggregatorUsesMatchedDomainAndCapturesErrorAndLatencyDimensions(t *testing.T) {
	store := &contributionStoreStub{}
	aggregator := NewAggregatorWithSettings(store, "11111111-1111-1111-1111-111111111111", false, 0, true)
	when := time.Now().UTC()
	if err := aggregator.Flush(context.Background(), []Event{{Host: "untrusted.invalid", Domain: "configured.example", Status: 604, StatusSource: "YaginxProxy", ErrorCategory: "UpstreamRequestFailure", DurationMS: 280, At: when}}); err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 2 {
		t.Fatalf("contributions=%#v", store.values)
	}
	for _, contribution := range store.values {
		if contribution.Host != "configured.example" || contribution.ErrorCounts["yaginxproxy:upstreamrequestfailure"] != 1 || contribution.LatencyHistogram["<=500"] != 1 {
			t.Fatalf("contribution=%#v", contribution)
		}
	}
}

func TestAggregatorReopensLateFinalizedPeriodWithNewSegment(t *testing.T) {
	store := &contributionStoreStub{}
	aggregator := NewAggregatorWithSettings(store, "11111111-1111-1111-1111-111111111111", false, 0, true)
	when := time.Date(2000, 7, 28, 3, 0, 0, 0, time.UTC)
	if err := aggregator.Flush(context.Background(), []Event{{Host: "late.example", At: when, Status: 200}}); err != nil {
		t.Fatal(err)
	}
	first := append([]Contribution(nil), store.values...)
	if err := aggregator.Flush(context.Background(), []Event{{Host: "late.example", At: when, Status: 500}}); err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(store.values) != 2 {
		t.Fatalf("first=%d latest=%d", len(first), len(store.values))
	}
	if first[0].ProcessID == store.values[0].ProcessID && first[1].ProcessID == store.values[1].ProcessID {
		t.Fatalf("late event reused finalized segment: first=%#v latest=%#v", first, store.values)
	}
}

type blockingContributionStore struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	values  []Contribution
}

func (s *blockingContributionStore) SaveTrafficContributions(_ context.Context, values []Contribution) error {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-s.release
	s.mu.Lock()
	s.values = append(s.values, values...)
	s.mu.Unlock()
	return nil
}

func TestAggregatorConsumesNewEventsWhilePersistenceIsBlocked(t *testing.T) {
	store := &blockingContributionStore{started: make(chan struct{}), release: make(chan struct{})}
	aggregator := NewAggregatorWithSettings(store, "node", false, 0, true)
	aggregator.FlushAggregation(context.Background(), []AggregationEvent{{Host: "async.example", At: time.Now(), Status: 200}})
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("persistence did not start")
	}
	done := make(chan struct{})
	go func() {
		_ = aggregator.FlushAggregation(context.Background(), []AggregationEvent{{Host: "async.example", At: time.Now(), Status: 201}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("aggregation event was blocked by database persistence")
	}
	close(store.release)
}
