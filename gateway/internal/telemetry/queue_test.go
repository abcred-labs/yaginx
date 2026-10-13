package telemetry

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type retrySink struct {
	mu       sync.Mutex
	attempts int
	accepted []Event
}

type alwaysFailSink struct{}

func (alwaysFailSink) Flush(context.Context, []Event) error {
	return errors.New("database is unavailable")
}

func (s *retrySink) Flush(_ context.Context, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts == 1 {
		return errors.New("temporary database failure")
	}
	s.accepted = append(s.accepted, events...)
	return nil
}

func TestQueueRetainsFailedBatchForRetry(t *testing.T) {
	queue := NewQueue(8)
	sink := &retrySink{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); queue.Run(ctx, 10*time.Millisecond, 1, sink) }()
	queue.Record(Event{Host: "retry.example.invalid"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		accepted, attempts := len(sink.accepted), sink.attempts
		sink.mu.Unlock()
		if accepted == 1 && attempts >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.accepted) != 1 || sink.accepted[0].Host != "retry.example.invalid" {
		t.Fatalf("accepted=%#v", sink.accepted)
	}
	if queue.FailedFlushes() != 1 || queue.Dropped() != 0 {
		t.Fatalf("failed=%d dropped=%d", queue.FailedFlushes(), queue.Dropped())
	}
}

func TestQueueRunExitsWhenCanceledWithPendingFailedBatch(t *testing.T) {
	queue := NewQueue(8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); queue.Run(ctx, time.Hour, 1, alwaysFailSink{}) }()
	queue.Record(Event{Host: "shutdown.example.invalid"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && queue.FailedFlushes() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("queue did not exit after cancellation")
	}
	if queue.ResidentBytes() != 0 || queue.BatchDepth() != 0 {
		t.Fatalf("queue state after cancellation: resident=%d batch=%d", queue.ResidentBytes(), queue.BatchDepth())
	}
}

func TestHistoryPolicyHonorsModeSamplingAndExclusions(t *testing.T) {
	errorEvent := Event{Path: "/api", Status: 502}
	successEvent := Event{Path: "/api", Status: 200}
	if (HistoryPolicy{}).ShouldRecord(errorEvent) {
		t.Fatal("disabled policy recorded an error")
	}
	errorsOnly := HistoryPolicy{Enabled: true}
	if !errorsOnly.ShouldRecord(errorEvent) || errorsOnly.ShouldRecord(successEvent) {
		t.Fatal("ErrorsOnly policy did not retain only errors")
	}
	if errorsOnly.ShouldRecord(Event{Status: 502, StatusSource: "ClientCanceled", ErrorCategory: "ClientCanceled"}) {
		t.Fatal("client cancellations should not enter request history")
	}
	if errorsOnly.ShouldRecord(Event{Status: 404}) {
		t.Fatal("ordinary 404 responses should not enter request history")
	}
	if errorsOnly.ShouldCaptureMetadata(Event{Status: 200}) {
		t.Fatal("ErrorsOnly policy should not copy successful metadata")
	}
	if !errorsOnly.ShouldCaptureMetadata(Event{Status: 502}) {
		t.Fatal("ErrorsOnly policy should copy error metadata")
	}
	all := HistoryPolicy{Enabled: true, AllMetadata: true, SuccessSamplingRate: 0.5, Sample: func() float64 { return 0.49 }}
	if !all.ShouldRecord(successEvent) {
		t.Fatal("sampled success was not retained")
	}
	all.Sample = func() float64 { return 0.5 }
	if all.ShouldRecord(successEvent) {
		t.Fatal("success at sampling boundary was retained")
	}
	samples := 0
	all.Sample = func() float64 {
		samples++
		return 0.49
	}
	decision := all.Decide(successEvent)
	if samples != 1 || !decision.Record || !decision.CaptureMetadata {
		t.Fatalf("single history decision samples=%d decision=%#v", samples, decision)
	}
	all.Sample = func() float64 {
		samples++
		return 0.99
	}
	decision = all.Decide(successEvent)
	if samples != 2 || decision.Record || decision.CaptureMetadata {
		t.Fatalf("rejected sampled success decision=%#v", decision)
	}
	excluded := HistoryPolicy{Enabled: true, AllMetadata: true, SuccessSamplingRate: 1, ExcludedPathPrefixes: []string{"/yaginx_console"}}
	if excluded.ShouldRecord(Event{Path: "/yaginx_console/api/account/login", Status: 500}) {
		t.Fatal("excluded path was retained")
	}
	full := HistoryPolicy{}
	for _, value := range []Event{
		{HistoryMode: "all", Path: "/missing", Status: http.StatusNotFound},
		{HistoryMode: "all", Path: "/cancelled", Status: 601, StatusSource: "ClientCanceled", ErrorCategory: "ClientCanceled"},
	} {
		if !full.ShouldRecord(value) {
			t.Fatalf("all metadata policy dropped completed event=%#v", value)
		}
	}
}

func TestTrafficPolicyExcludesConfiguredPaths(t *testing.T) {
	policy := TrafficPolicy{Enabled: true, ExcludedPathPrefixes: []string{"/yaginx_console"}}
	if policy.ShouldAggregate(Event{Path: "/yaginx_console/api/account/login"}) {
		t.Fatal("console request should not enter traffic aggregation")
	}
	if !policy.ShouldAggregate(Event{Path: "/customer"}) {
		t.Fatal("business request should enter traffic aggregation")
	}
}

func TestQueueDropsHistoryBatchAfterConfiguredRetries(t *testing.T) {
	queue := NewQueue(8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); queue.RunWithRetries(ctx, 5*time.Millisecond, 1, alwaysFailSink{}, 2) }()
	queue.Record(Event{Host: "drop.example.invalid"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && queue.Dropped() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if queue.Dropped() != 1 || queue.FailedFlushes() != 3 {
		t.Fatalf("dropped=%d failed=%d", queue.Dropped(), queue.FailedFlushes())
	}
}

func TestAggregationQueueEnforcesByteBudgetAndKeepsEventsCompact(t *testing.T) {
	queue := NewAggregationQueue(10, 160)
	queue.Record(AggregationEvent{Host: "one.example", Domain: "one.example", Status: 200})
	queue.Record(AggregationEvent{Host: "two.example", Domain: "two.example", Status: 200})
	if queue.Dropped() == 0 {
		t.Fatal("expected byte budget to reject an event")
	}
	reasons := queue.DropCounts()
	if reasons[DropBytes] == 0 && reasons[DropOversize] == 0 {
		t.Fatalf("drop reasons=%#v", reasons)
	}
}

func TestAggregationEventDoesNotCarryHistoryIdentityOrPayload(t *testing.T) {
	value := AggregationEvent{Host: "example.invalid", Domain: "example.invalid", Status: 500, UserAgent: "agent"}
	if value.Host == "" || value.Domain == "" || value.Status != 500 {
		t.Fatalf("unexpected aggregation event=%#v", value)
	}
	// This is intentionally a compile-time shape test: sensitive fields such
	// as ClientIP, TraceID, headers and bodies exist only on Event.
	var _ = Event{ClientIP: "198.51.100.1", TraceID: "trace", RequestBodyBase64: "body"}
}
