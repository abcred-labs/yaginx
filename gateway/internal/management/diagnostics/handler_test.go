package diagnostics

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"yaginx-gateway/internal/management"
)

type diagnosticsStoreStub struct {
	health management.GatewayHealthSummary
	cutoff int64
}

func (s *diagnosticsStoreStub) DatabasePerformance(context.Context) (management.DatabasePerformance, error) {
	return management.DatabasePerformance{}, nil
}
func (s *diagnosticsStoreStub) GatewayNodeHealthSummary(_ context.Context, cutoff int64) (management.GatewayHealthSummary, error) {
	s.cutoff = cutoff
	return s.health, nil
}

func TestOverviewUsesAggregatedNodeHealth(t *testing.T) {
	store := &diagnosticsStoreStub{health: management.GatewayHealthSummary{
		TotalNodes: 250, ActiveNodes: 200, StaleNodes: 30, DrainingNodes: 20, LatestHeartbeat: 123,
	}}
	handler := Handler{Store: store, Observe: management.ObserveRequest(func(string, string, time.Duration) {})}
	recorder := httptest.NewRecorder()
	handler.Overview(recorder, httptest.NewRequest("GET", "/api/diagnostics/overview", nil))
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.cutoff == 0 {
		t.Fatal("expected handler to pass a heartbeat cutoff")
	}
}
