package telemetry

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"yaginx-gateway/internal/platform/hostnorm"
	proxyevent "yaginx-gateway/internal/proxy/event"
)

type Contribution struct {
	NodeID, ProcessID   string
	Host                string
	Cycle               int
	Period              int64
	Requests            int64
	DurationSum         int64
	DurationMax         int64
	Inbound             int64
	Outbound            int64
	StatusCounts        map[string]int64
	SpiderCounts        map[string]int64
	BrowserCounts       map[string]int64
	OSCounts            map[string]int64
	DurationBuckets     map[string]int64
	ErrorCounts         map[string]int64
	LatencyHistogram    map[string]int64
	ResponseModeMetrics map[string]map[string]int64
	IsFinal             bool
}

type ContributionStore interface {
	SaveTrafficContributions(context.Context, []Contribution) error
}

type HistoryStore interface {
	FlushRequestHistory(context.Context, []Event) error
}

type BlacklistHitStore interface {
	FlushBlacklistHits(context.Context, []Event) error
}

// HistorySink keeps request-history persistence independent from the traffic
// contribution cadence. The proxy selects events before they enter this sink.
type HistorySink struct {
	Store  HistoryStore
	Parser *UserAgentParser
}

func (s HistorySink) Flush(ctx context.Context, events []Event) error {
	if s.Store == nil {
		return nil
	}
	started := time.Now()
	parsed, unknown, failed := 0, 0, 0
	if s.Parser != nil {
		for i := range events {
			events[i].UserAgentInfo = s.Parser.Parse(events[i].UserAgent)
			if events[i].UserAgentInfo.ParseFailed {
				failed++
				continue
			}
			switch events[i].UserAgentInfo.ParseStatus {
			case "parsed":
				parsed++
			case "unknown":
				unknown++
			case "empty", "disabled":
			default:
				failed++
			}
		}
	}
	if s.Parser != nil {
		slog.Debug("request history User-Agent parsing", "events", len(events), "parsed", parsed, "unknown", unknown, "failed", failed, "duration_ms", time.Since(started).Milliseconds())
	}
	return s.Store.FlushRequestHistory(ctx, events)
}

// BlacklistHitSink persists security counters even when request history is
// disabled or sampled.
type BlacklistHitSink struct{ Store BlacklistHitStore }

func (s BlacklistHitSink) Flush(ctx context.Context, events []Event) error {
	if s.Store == nil {
		return nil
	}
	return s.Store.FlushBlacklistHits(ctx, events)
}

// Aggregator converts transient events into process-local absolute hourly
// snapshots. Replaying the same snapshot is safe because the database row is
// replaced, not incremented.
type Aggregator struct {
	store                ContributionStore
	nodeID               string
	processID            string
	ua                   *userAgentClassifier
	includeCurrentPeriod bool
	lateWindow           time.Duration
	maxDirtyBuckets      int
	mu                   sync.Mutex
	values               map[string]Contribution
	segments             map[string]string
	evicted              map[string]bool
	dirty                map[string]Contribution
	versions             map[string]uint64
	nextRetry            time.Time
	retryBackoff         time.Duration
	onPeriods            func([]PeriodHint)
	persistRunning       bool
	persistenceFailures  atomic.Uint64
	lastPersistNanos     atomic.Int64
}

type AggregatorStats struct {
	Buckets, DirtyBuckets int
	PersistenceFailures   uint64
	LastPersistDuration   time.Duration
}

func (a *Aggregator) Stats() AggregatorStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AggregatorStats{
		Buckets: len(a.values), DirtyBuckets: len(a.dirty),
		PersistenceFailures: a.persistenceFailures.Load(),
		LastPersistDuration: time.Duration(a.lastPersistNanos.Load()),
	}
}

type PeriodHint struct {
	Host   string
	Cycle  int
	Period int64
}

func NewAggregator(store ContributionStore, nodeID string) *Aggregator {
	return NewAggregatorWithUserAgent(store, nodeID, false, 0)
}

// NewAggregatorWithUserAgent retains the legacy optional UA dimensions without
// ever putting parsing or cache synchronization on the proxy request path.
func NewAggregatorWithUserAgent(store ContributionStore, nodeID string, enabled bool, cacheSize int) *Aggregator {
	return NewAggregatorWithSettings(store, nodeID, enabled, cacheSize, true)
}

func NewAggregatorWithSettings(store ContributionStore, nodeID string, uaEnabled bool, uaCacheSize int, includeCurrentPeriod bool) *Aggregator {
	return &Aggregator{
		store: store, nodeID: nodeID, processID: uuid.NewString(),
		values: map[string]Contribution{}, segments: map[string]string{}, evicted: map[string]bool{},
		dirty: map[string]Contribution{}, versions: map[string]uint64{},
		ua:                   newUserAgentClassifier(uaEnabled, uaCacheSize),
		includeCurrentPeriod: includeCurrentPeriod, lateWindow: 20 * time.Minute, maxDirtyBuckets: 10000,
	}
}

// SetPeriodHintSink receives deduplicated periods after their contribution
// snapshots are durable. The callback must be non-blocking.
func (a *Aggregator) SetPeriodHintSink(sink func([]PeriodHint)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onPeriods = sink
}

func (a *Aggregator) Flush(ctx context.Context, events []Event) error {
	compact := make([]AggregationEvent, 0, len(events))
	for _, value := range events {
		status := value.HistoryStatus
		if status == 0 {
			status = value.Status
		}
		compact = append(compact, AggregationEvent{
			Host: value.Host, Domain: value.Domain, At: value.At, Status: status,
			StatusSource: value.StatusSource, ErrorCategory: value.ErrorCategory,
			DurationMS: value.DurationMS, Inbound: value.Inbound, Outbound: value.Outbound, UserAgent: value.UserAgent,
			ResponseMode: value.ResponseMode, TTFBMS: value.TTFBMS, TTFTMS: value.TTFTMS,
		})
	}
	return a.applyAggregation(ctx, compact, true)
}

func (a *Aggregator) FlushAggregation(ctx context.Context, events []AggregationEvent) error {
	return a.applyAggregation(ctx, events, false)
}

func (a *Aggregator) applyAggregation(ctx context.Context, events []AggregationEvent, synchronous bool) error {
	a.mu.Lock()
	hints := make([]PeriodHint, 0, len(events)*2)
	for _, event := range events {
		host := event.Domain
		if host == "" {
			host = normalizedDomain(event.Host)
		}
		if host == "" {
			host = "__unmatched__"
		}
		for _, bucket := range []struct {
			cycle  int
			period int64
		}{{5, event.At.UTC().Truncate(time.Minute).Unix()}, {10, event.At.UTC().Truncate(time.Hour).Unix()}} {
			base := host + "\x00" + strconv.Itoa(bucket.cycle) + "\x00" + strconv.FormatInt(bucket.period, 10)
			key := a.segments[base]
			if key == "" {
				segment := a.processID
				if a.evicted[base] {
					segment = uuid.NewString()
				}
				key = base + "\x00" + segment
				a.segments[base] = key
				delete(a.evicted, base)
			}
			value := a.values[key]
			if value.Host == "" {
				segment := strings.TrimPrefix(key, base+"\x00")
				value = newContribution(a.nodeID, segment, host, bucket.cycle, bucket.period)
			}
			value.Requests++
			value.DurationSum += event.DurationMS
			if event.DurationMS > value.DurationMax {
				value.DurationMax = event.DurationMS
			}
			value.Inbound += event.Inbound
			value.Outbound += event.Outbound
			value.StatusCounts[statusKey(event.Status)]++
			if error := errorKey(event, event.Status); error != "" {
				value.ErrorCounts[error]++
			}
			value.DurationBuckets[durationBucket(event.DurationMS)]++
			value.LatencyHistogram[latencyBucket(event.DurationMS)]++
			mode := normalizeResponseMode(event.ResponseMode)
			modeMetrics := value.ResponseModeMetrics[mode]
			modeMetrics["requests"]++
			modeMetrics["duration_sum"] += event.DurationMS
			if event.DurationMS > modeMetrics["duration_max"] {
				modeMetrics["duration_max"] = event.DurationMS
			}
			modeMetrics["duration_hist:"+latencyBucket(event.DurationMS)]++
			if event.TTFBMS > 0 {
				modeMetrics["ttfb_sum"] += event.TTFBMS
				modeMetrics["ttfb_count"]++
				modeMetrics["ttfb_hist:"+latencyBucket(event.TTFBMS)]++
			}
			if event.TTFTMS > 0 {
				modeMetrics["ttft_sum"] += event.TTFTMS
				modeMetrics["ttft_count"]++
				modeMetrics["ttft_hist:"+latencyBucket(event.TTFTMS)]++
			}
			value.ResponseModeMetrics[mode] = modeMetrics
			if a.ua.enabled {
				ua := a.ua.parse(event.UserAgent)
				if ua.browser == "" {
					ua.browser = "Unknown"
				}
				if ua.os == "" {
					ua.os = "Unknown"
				}
				if ua.spider {
					value.SpiderCounts[ua.browser]++
				} else {
					value.BrowserCounts[ua.browser]++
				}
				value.OSCounts[ua.os]++
			}
			a.values[key] = value
			a.dirty[key] = value
			a.versions[key]++
			hints = append(hints, PeriodHint{Host: host, Cycle: bucket.cycle, Period: bucket.period})
		}
	}
	finalizeContributionPeriods(a.values, time.Now().UTC(), a.lateWindow)
	for key, value := range a.values {
		if value.IsFinal {
			a.dirty[key] = value
		}
	}
	if len(a.dirty) > a.maxDirtyBuckets {
		a.dropOldestDirtyLocked()
	}
	shouldPersist := a.hasPersistableDirtyLocked()
	if !synchronous && shouldPersist && !a.persistRunning {
		a.persistRunning = true
		go a.persistOnce(ctx)
	}
	a.mu.Unlock()
	if synchronous && shouldPersist {
		return a.persistOnce(ctx)
	}
	return nil
}

func (a *Aggregator) hasPersistableDirtyLocked() bool {
	if a.store == nil || (!a.nextRetry.IsZero() && time.Now().Before(a.nextRetry)) {
		return false
	}
	for _, value := range a.dirty {
		if a.includeCurrentPeriod || value.IsFinal {
			return true
		}
	}
	return false
}

func (a *Aggregator) persistOnce(ctx context.Context) error {
	a.mu.Lock()
	if !a.hasPersistableDirtyLocked() {
		a.persistRunning = false
		a.mu.Unlock()
		return nil
	}
	snapshots := make([]Contribution, 0, len(a.dirty))
	versions := make(map[string]uint64, len(a.dirty))
	hints := make([]PeriodHint, 0, len(a.dirty))
	for key, value := range a.dirty {
		if !a.includeCurrentPeriod && !value.IsFinal {
			continue
		}
		snapshots = append(snapshots, cloneContribution(value))
		versions[key] = a.versions[key]
		hints = append(hints, PeriodHint{Host: value.Host, Cycle: value.Cycle, Period: value.Period})
	}
	store := a.store
	a.mu.Unlock()

	started := time.Now()
	err := store.SaveTrafficContributions(ctx, snapshots)
	a.lastPersistNanos.Store(time.Since(started).Nanoseconds())
	a.mu.Lock()
	if err != nil {
		a.persistenceFailures.Add(1)
		if a.retryBackoff == 0 {
			a.retryBackoff = time.Second
		} else if a.retryBackoff < 30*time.Second {
			a.retryBackoff *= 2
		}
		if a.retryBackoff > 30*time.Second {
			a.retryBackoff = 30 * time.Second
		}
		a.nextRetry = time.Now().Add(a.retryBackoff)
		a.persistRunning = false
		retryAfter := a.retryBackoff
		retryContext := ctx
		time.AfterFunc(retryAfter, func() {
			if retryContext.Err() != nil {
				return
			}
			a.mu.Lock()
			if !a.persistRunning && a.hasPersistableDirtyLocked() {
				a.persistRunning = true
				go a.persistOnce(retryContext)
			}
			a.mu.Unlock()
		})
		a.mu.Unlock()
		return err
	}
	for key, version := range versions {
		if a.versions[key] == version {
			delete(a.dirty, key)
		}
	}
	a.nextRetry = time.Time{}
	a.retryBackoff = 0
	a.persistRunning = false
	a.evictFinalizedLocked(time.Now().UTC())
	sink := a.onPeriods
	if a.hasPersistableDirtyLocked() {
		a.persistRunning = true
		go a.persistOnce(ctx)
	}
	a.mu.Unlock()
	if sink != nil && len(hints) > 0 {
		sink(uniquePeriodHints(hints))
	}
	return nil
}

func finalizeContributionPeriods(values map[string]Contribution, now time.Time, _ time.Duration) {
	minute := now.UTC().Truncate(time.Minute).Unix()
	hour := now.UTC().Truncate(time.Hour).Unix()
	for key, value := range values {
		if (value.Cycle == 5 && value.Period < minute) || (value.Cycle == 10 && value.Period < hour) {
			value.IsFinal = true
			values[key] = value
		}
	}
}

func (a *Aggregator) evictFinalizedLocked(now time.Time) {
	cutoff := now.Add(-a.lateWindow)
	for key, value := range a.values {
		if !value.IsFinal || time.Unix(value.Period, 0).UTC().After(cutoff) {
			continue
		}
		if _, pending := a.dirty[key]; pending {
			continue
		}
		base := value.Host + "\x00" + strconv.Itoa(value.Cycle) + "\x00" + strconv.FormatInt(value.Period, 10)
		delete(a.values, key)
		delete(a.segments, base)
		a.evicted[base] = true
	}
}

func (a *Aggregator) dropOldestDirtyLocked() {
	for len(a.dirty) > a.maxDirtyBuckets {
		var oldestKey string
		var oldest int64
		var foundFinal bool
		for key, value := range a.dirty {
			if !value.IsFinal && foundFinal {
				continue
			}
			if value.IsFinal && !foundFinal {
				oldestKey, oldest, foundFinal = key, value.Period, true
				continue
			}
			if oldestKey == "" || value.Period < oldest {
				oldestKey, oldest = key, value.Period
			}
		}
		if oldestKey == "" {
			return
		}
		value := a.dirty[oldestKey]
		base := value.Host + "\x00" + strconv.Itoa(value.Cycle) + "\x00" + strconv.FormatInt(value.Period, 10)
		delete(a.dirty, oldestKey)
		delete(a.values, oldestKey)
		delete(a.segments, base)
		a.evicted[base] = true
	}
}

func uniquePeriodHints(values []PeriodHint) []PeriodHint {
	seen := make(map[string]struct{}, len(values))
	out := values[:0]
	for _, value := range values {
		key := value.Host + "\x00" + strconv.Itoa(value.Cycle) + "\x00" + strconv.FormatInt(value.Period, 10)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneContribution(value Contribution) Contribution {
	copy := value
	copy.StatusCounts = cloneCounts(value.StatusCounts)
	copy.SpiderCounts = cloneCounts(value.SpiderCounts)
	copy.BrowserCounts = cloneCounts(value.BrowserCounts)
	copy.OSCounts = cloneCounts(value.OSCounts)
	copy.DurationBuckets = cloneCounts(value.DurationBuckets)
	copy.ErrorCounts = cloneCounts(value.ErrorCounts)
	copy.LatencyHistogram = cloneCounts(value.LatencyHistogram)
	copy.ResponseModeMetrics = cloneNestedCounts(value.ResponseModeMetrics)
	return copy
}

func newContribution(nodeID, processID, host string, cycle int, period int64) Contribution {
	return Contribution{NodeID: nodeID, ProcessID: processID, Host: host, Cycle: cycle, Period: period, StatusCounts: map[string]int64{}, SpiderCounts: map[string]int64{}, BrowserCounts: map[string]int64{}, OSCounts: map[string]int64{}, DurationBuckets: map[string]int64{}, ErrorCounts: map[string]int64{}, LatencyHistogram: map[string]int64{}, ResponseModeMetrics: map[string]map[string]int64{proxyevent.ResponseModeNonStream: {}, proxyevent.ResponseModeStream: {}, proxyevent.ResponseModeUpgrade: {}}}
}

func normalizeResponseMode(mode string) string {
	switch mode {
	case proxyevent.ResponseModeStream, proxyevent.ResponseModeUpgrade:
		return mode
	default:
		return proxyevent.ResponseModeNonStream
	}
}
func cloneNestedCounts(value map[string]map[string]int64) map[string]map[string]int64 {
	out := make(map[string]map[string]int64, len(value))
	for key, counts := range value {
		out[key] = cloneCounts(counts)
	}
	return out
}

func normalizedDomain(host string) string {
	host, _ = hostnorm.HostHeader(host)
	return host
}

func errorKey(event AggregationEvent, status int) string {
	if event.ErrorCategory != "" {
		return strings.ToLower(strings.TrimSpace(event.StatusSource)) + ":" + strings.ToLower(strings.TrimSpace(event.ErrorCategory))
	}
	switch {
	case status >= 600:
		return "gateway:proxy_error"
	case status >= 500:
		return "upstream:server_error"
	case status >= 400:
		return "client:request_error"
	default:
		return ""
	}
}

func cloneCounts(source map[string]int64) map[string]int64 {
	copy := make(map[string]int64, len(source))
	for key, count := range source {
		copy[key] = count
	}
	return copy
}

func durationBucket(durationMS int64) string {
	switch {
	case durationMS <= 100:
		return "<=100"
	case durationMS <= 500:
		return ">100 & <=500"
	case durationMS <= 1000:
		return ">500 & <=1000"
	default:
		return ">1000"
	}
}

var latencyBoundaries = []int64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000}

// latencyBucket is deliberately fixed and mergeable across nodes and report
// grains. The management API interpolates its P50/P95/P99 values and labels
// them as estimates rather than presenting the old average/max pair as a
// percentile.
func latencyBucket(durationMS int64) string {
	if durationMS < 0 {
		durationMS = 0
	}
	for _, boundary := range latencyBoundaries {
		if durationMS <= boundary {
			return "<=" + strconv.FormatInt(boundary, 10)
		}
	}
	return "+Inf"
}

func statusKey(status int) string {
	if status < 0 {
		return "0"
	}
	return strconv.Itoa(status)
}
