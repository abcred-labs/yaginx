// Package telemetry contains deliberately asynchronous request event delivery.
package telemetry

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"yaginx-gateway/internal/proxy/event"
)

type Event = event.Event
type AggregationEvent = event.AggregationEvent
type HistoryDecision = event.HistoryDecision

type HistoryPolicy struct {
	Enabled, AllMetadata bool
	includeAllOutcomes   bool
	SuccessSamplingRate  float64
	ExcludedPathPrefixes []string
	Sample               func() float64
}

type TrafficPolicy struct {
	Enabled              bool
	ExcludedPathPrefixes []string
}

func (p TrafficPolicy) ShouldAggregate(value Event) bool {
	return p.Enabled && !excludedPath(value.Path, p.ExcludedPathPrefixes)
}

func (p HistoryPolicy) ShouldRecord(value Event) bool {
	return p.Decide(value).Record
}

// Decide makes the persistence and metadata decision once. In particular,
// successful sampling is evaluated only here, so callers can reuse the result
// for both queueing and metadata construction.
func (p HistoryPolicy) Decide(value Event) HistoryDecision {
	switch value.HistoryMode {
	case "errorsOnly":
		p.Enabled, p.AllMetadata, p.includeAllOutcomes = true, false, false
	case "all":
		p.Enabled, p.AllMetadata, p.includeAllOutcomes = true, true, true
		p.SuccessSamplingRate = 1
		if value.HistorySuccessSamplingRate != nil {
			p.SuccessSamplingRate = *value.HistorySuccessSamplingRate
		}
	}
	if !p.Enabled || excludedPath(value.Path, p.ExcludedPathPrefixes) {
		return HistoryDecision{}
	}
	status := value.HistoryStatus
	if status == 0 {
		status = value.Status
	}
	// Inherited policies retain legacy noise suppression. An explicit site all
	// policy includes these outcomes even when successful requests are sampled.
	if !p.includeAllOutcomes {
		if value.StatusSource == "ClientCanceled" || value.ErrorCategory == "ClientCanceled" {
			return HistoryDecision{}
		}
		if status == http.StatusNotFound && value.ErrorCategory == "" {
			return HistoryDecision{}
		}
	}
	if status >= 400 || value.ErrorCategory != "" {
		return HistoryDecision{Record: true, CaptureMetadata: true}
	}
	if !p.AllMetadata || p.SuccessSamplingRate <= 0 {
		return HistoryDecision{}
	}
	if p.SuccessSamplingRate >= 1 {
		return HistoryDecision{Record: true, CaptureMetadata: true}
	}
	sample := p.Sample
	if sample == nil {
		sample = rand.Float64
	}
	if sample() < p.SuccessSamplingRate {
		return HistoryDecision{Record: true, CaptureMetadata: true}
	}
	return HistoryDecision{}
}

// ShouldCaptureMetadata decides whether request and response metadata is
// worth copying into the completed event. Error events are always retained;
// successful metadata is only eligible when success sampling is enabled.
func (p HistoryPolicy) ShouldCaptureMetadata(value Event) bool {
	return p.Decide(value).CaptureMetadata
}

func excludedPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

type Sink interface {
	Flush(context.Context, []Event) error
}

type AggregationSink interface {
	FlushAggregation(context.Context, []AggregationEvent) error
}

type DropReason string

const (
	DropFull               DropReason = "full"
	DropBytes              DropReason = "bytes"
	DropOversize           DropReason = "oversize"
	DropPersistenceBacklog DropReason = "persistence_backlog"
	DropRetry              DropReason = "retry"
)

type queueStats struct {
	dropped, failed                           atomic.Uint64
	full, bytes, oversize, persistence, retry atomic.Uint64
	residentBytes, batchDepth                 atomic.Int64
}

func (s *queueStats) drop(reason DropReason) {
	s.dropped.Add(1)
	switch reason {
	case DropFull:
		s.full.Add(1)
	case DropBytes:
		s.bytes.Add(1)
	case DropOversize:
		s.oversize.Add(1)
	case DropPersistenceBacklog:
		s.persistence.Add(1)
	case DropRetry:
		s.retry.Add(1)
	}
}

// queueCore owns the type-independent bounded-buffer behavior. The runner
// below is deliberately generic so history and aggregation queues cannot
// drift in their accounting or cancellation semantics.
type queueCore[T any] struct {
	events   chan T
	maxBytes int64
	sizeOf   func(T) int
	stats    queueStats
}

func newQueueCore[T any](capacity int, maxBytes int64, sizeOf func(T) int) *queueCore[T] {
	if capacity < 1 {
		capacity = 1
	}
	if maxBytes < 1 {
		maxBytes = 64 << 20
	}
	return &queueCore[T]{events: make(chan T, capacity), maxBytes: maxBytes, sizeOf: sizeOf}
}

func (q *queueCore[T]) record(value T) {
	size := q.sizeOf(value)
	if int64(size) > q.maxBytes {
		q.stats.drop(DropOversize)
		return
	}
	if !reserveBytes(&q.stats.residentBytes, int64(size), q.maxBytes) {
		q.stats.drop(DropBytes)
		return
	}
	select {
	case q.events <- value:
	default:
		q.stats.residentBytes.Add(-int64(size))
		q.stats.drop(DropFull)
	}
}

func (q *queueCore[T]) dropped() uint64       { return q.stats.dropped.Load() }
func (q *queueCore[T]) failedFlushes() uint64 { return q.stats.failed.Load() }
func (q *queueCore[T]) depth() int            { return len(q.events) }
func (q *queueCore[T]) residentBytes() int64  { return q.stats.residentBytes.Load() }
func (q *queueCore[T]) batchDepth() int       { return int(q.stats.batchDepth.Load()) }
func (q *queueCore[T]) dropCounts() map[DropReason]uint64 {
	return map[DropReason]uint64{
		DropFull: q.stats.full.Load(), DropBytes: q.stats.bytes.Load(),
		DropOversize: q.stats.oversize.Load(), DropPersistenceBacklog: q.stats.persistence.Load(),
		DropRetry: q.stats.retry.Load(),
	}
}

func (q *queueCore[T]) run(ctx context.Context, interval time.Duration, batchSize, maxFlushRetries int, flush func(context.Context, []T) error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if batchSize < 1 {
		batchSize = 1
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	batch := make([]T, 0, batchSize)
	retries := 0
	nextRetry := time.Time{}
	discardBatch := func() {
		if len(batch) == 0 {
			q.stats.batchDepth.Store(0)
			return
		}
		q.stats.drop(DropRetry)
		q.stats.residentBytes.Add(-batchBytesOf(batch, q.sizeOf))
		clear(batch)
		batch = batch[:0]
		retries = 0
		nextRetry = time.Time{}
		q.stats.batchDepth.Store(0)
	}
	flushBatch := func() bool {
		if ctx.Err() != nil {
			discardBatch()
			return true
		}
		if len(batch) == 0 || (!nextRetry.IsZero() && time.Now().Before(nextRetry)) {
			return len(batch) == 0
		}
		q.stats.batchDepth.Store(int64(len(batch)))
		if err := flush(ctx, batch); err != nil {
			q.stats.failed.Add(1)
			retries++
			if maxFlushRetries >= 0 && retries > maxFlushRetries {
				q.stats.drop(DropRetry)
				q.stats.residentBytes.Add(-batchBytesOf(batch, q.sizeOf))
				clear(batch)
				batch = batch[:0]
				retries = 0
				nextRetry = time.Time{}
				q.stats.batchDepth.Store(0)
				return true
			}
			base := interval
			if base > time.Second {
				base = time.Second
			}
			delay := base << min(retries-1, 5)
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			nextRetry = time.Now().Add(delay)
			return false
		}
		q.stats.residentBytes.Add(-batchBytesOf(batch, q.sizeOf))
		clear(batch)
		batch = batch[:0]
		retries = 0
		nextRetry = time.Time{}
		q.stats.batchDepth.Store(0)
		return true
	}
	for {
		select {
		case value := <-q.events:
			q.stats.residentBytes.Add(-int64(q.sizeOf(value)))
			if len(batch) >= batchSize && !flushBatch() {
				if q.stats.residentBytes.Load()+int64(q.sizeOf(value)) <= q.maxBytes {
					batch = append(batch, value)
					q.stats.residentBytes.Add(int64(q.sizeOf(value)))
				} else {
					q.stats.drop(DropPersistenceBacklog)
				}
				continue
			}
			batch = append(batch, value)
			q.stats.residentBytes.Add(int64(q.sizeOf(value)))
			if len(batch) >= batchSize {
				flushBatch()
			}
		case <-ticker.C:
			flushBatch()
		case <-ctx.Done():
			discardBatch()
			return
		}
	}
}

type Queue struct{ *queueCore[Event] }

func NewQueue(capacity int) *Queue { return NewQueueWithLimits(capacity, 64<<20) }
func NewQueueWithLimits(capacity int, maxBytes int64) *Queue {
	return &Queue{newQueueCore(capacity, maxBytes, eventSize)}
}
func (q *Queue) Record(value Event)                { q.queueCore.record(value) }
func (q *Queue) Dropped() uint64                   { return q.dropped() }
func (q *Queue) FailedFlushes() uint64             { return q.failedFlushes() }
func (q *Queue) Depth() int                        { return q.depth() }
func (q *Queue) ResidentBytes() int64              { return q.residentBytes() }
func (q *Queue) BatchDepth() int                   { return q.batchDepth() }
func (q *Queue) DropCounts() map[DropReason]uint64 { return q.dropCounts() }
func (q *Queue) Run(ctx context.Context, interval time.Duration, batchSize int, sink Sink) {
	q.queueCore.run(ctx, interval, batchSize, -1, sink.Flush)
}
func (q *Queue) RunWithRetries(ctx context.Context, interval time.Duration, batchSize int, sink Sink, maxFlushRetries int) {
	q.queueCore.run(ctx, interval, batchSize, maxFlushRetries, sink.Flush)
}

type AggregationQueue struct {
	*queueCore[AggregationEvent]
}

func NewAggregationQueue(capacity int, maxBytes int64) *AggregationQueue {
	return &AggregationQueue{newQueueCore(capacity, maxBytes, aggregationEventSize)}
}
func (q *AggregationQueue) Record(value AggregationEvent)     { q.queueCore.record(value) }
func (q *AggregationQueue) Dropped() uint64                   { return q.dropped() }
func (q *AggregationQueue) FailedFlushes() uint64             { return q.failedFlushes() }
func (q *AggregationQueue) Depth() int                        { return q.depth() }
func (q *AggregationQueue) ResidentBytes() int64              { return q.residentBytes() }
func (q *AggregationQueue) BatchDepth() int                   { return q.batchDepth() }
func (q *AggregationQueue) DropCounts() map[DropReason]uint64 { return q.dropCounts() }
func (q *AggregationQueue) Run(ctx context.Context, interval time.Duration, batchSize int, sink AggregationSink) {
	q.queueCore.run(ctx, interval, batchSize, 0, sink.FlushAggregation)
}

func eventSize(value Event) int {
	size := 256 + len(value.Host) + len(value.Domain) + len(value.Path) + len(value.Query) + len(value.ClientIP) + len(value.TraceID) + len(value.UserAgent)
	size += len(value.RequestBodyBase64) + len(value.ResponseBodyBase64) + len(value.ExceptionMessage) + len(value.ExceptionStackTrace)
	for key, values := range value.RequestHeaders {
		size += len(key)
		for _, item := range values {
			size += len(item)
		}
	}
	for key, values := range value.ResponseHeaders {
		size += len(key)
		for _, item := range values {
			size += len(item)
		}
	}
	return size
}

func aggregationEventSize(value AggregationEvent) int {
	return 128 + len(value.Host) + len(value.Domain) + len(value.StatusSource) + len(value.ErrorCategory) + len(value.UserAgent)
}
func batchBytesOf[T any](values []T, sizeOf func(T) int) int64 {
	var total int64
	for _, value := range values {
		total += int64(sizeOf(value))
	}
	return total
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func reserveBytes(value *atomic.Int64, size, limit int64) bool {
	for {
		current := value.Load()
		if size > limit-current {
			return false
		}
		if value.CompareAndSwap(current, current+size) {
			return true
		}
	}
}
