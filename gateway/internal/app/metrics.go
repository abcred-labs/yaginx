package app

// Runtime gauges that no single component owns: the connection pool, the
// telemetry queues and the certificate inventory are each observed from here so
// the components themselves stay free of metric plumbing.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/observability"
	"yaginx-gateway/internal/sysevent"
	"yaginx-gateway/internal/telemetry"
)

type telemetryQueueMetrics interface {
	Depth() int
	Dropped() uint64
	FailedFlushes() uint64
	ResidentBytes() int64
	BatchDepth() int
	DropCounts() map[telemetry.DropReason]uint64
}

func runMetricsMaintenance(ctx context.Context, pool *pgxpool.Pool, traffic, history telemetryQueueMetrics, aggregator *telemetry.Aggregator, metrics *observability.Metrics, events sysevent.Recorder) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		started := time.Now()
		check, cancel := context.WithTimeout(ctx, time.Second)
		err := pool.Ping(check)
		cancel()
		if err != nil {
			metrics.ObserveDatabaseOperation("health", "failure", time.Since(started))
		} else {
			metrics.ObserveDatabaseOperation("health", "success", time.Since(started))
		}
		stat := pool.Stat()
		metrics.UpdatePool(stat.AcquiredConns(), stat.IdleConns(), stat.ConstructingConns(), stat.MaxConns())
		if traffic != nil {
			metrics.UpdateQueueMetrics("traffic", traffic.Depth(), traffic.BatchDepth(), traffic.ResidentBytes(), traffic.Dropped(), traffic.DropCounts())
		}
		if history != nil {
			metrics.UpdateQueueMetrics("history", history.Depth(), history.BatchDepth(), history.ResidentBytes(), history.Dropped(), history.DropCounts())
		}
		if aggregator != nil {
			metrics.UpdateAggregator(aggregator.Stats())
		}
		metrics.UpdateRuntimeMemory()
		// Only the aggregate telemetry queue is a Yaginx stability signal.
		// request_history and blacklist queues are proxy/request-domain data and
		// must never create system-error records.
		if traffic != nil {
			observeQueueFailures(ctx, events, "traffic", traffic)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

var queueFailureSeen sync.Map

func observeQueueFailures(ctx context.Context, events sysevent.Recorder, name string, queue telemetryQueueMetrics) {
	key := fmt.Sprintf("%p", queue)
	current := queue.Dropped() + queue.FailedFlushes()
	previous, _ := queueFailureSeen.LoadOrStore(key, uint64(0))
	if current <= previous.(uint64) {
		return
	}
	if events.Record(ctx, "telemetry", name, "warning", "TELEMETRY_QUEUE_FAILURE", errors.New("telemetry queue dropped or failed to flush events")) {
		queueFailureSeen.Store(key, current)
	}
}

func setCertificateMetrics(metrics *observability.Metrics, source string, entries []certificates.Entry) {
	buckets := map[string]int{"expired": 0, "under_7d": 0, "under_35d": 0, "over_35d": 0}
	now := time.Now().UTC()
	for _, entry := range entries {
		switch {
		case !entry.NotAfter.After(now):
			buckets["expired"]++
		case entry.NotAfter.Before(now.Add(7 * 24 * time.Hour)):
			buckets["under_7d"]++
		case entry.NotAfter.Before(now.Add(35 * 24 * time.Hour)):
			buckets["under_35d"]++
		default:
			buckets["over_35d"]++
		}
	}
	for bucket, count := range buckets {
		metrics.SetCertificateCount(source, "active", bucket, count)
	}
}
