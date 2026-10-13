// Package observability owns the low-cardinality Prometheus surface. It keeps
// labels deliberately bounded so a hostile Host/path/header cannot create an
// unbounded time-series set.
package observability

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"yaginx-gateway/internal/telemetry"
)

type Metrics struct {
	Registry               *prometheus.Registry
	proxyRequests          *prometheus.CounterVec
	proxyDuration          *prometheus.HistogramVec
	proxyBytes             *prometheus.CounterVec
	proxyActive            *prometheus.GaugeVec
	configRevision         *prometheus.GaugeVec
	configRefresh          *prometheus.CounterVec
	configRefreshDuration  *prometheus.HistogramVec
	configNotifyReconnect  *prometheus.CounterVec
	dbPoolConnections      *prometheus.GaugeVec
	dbOperationDuration    *prometheus.HistogramVec
	managementDuration     *prometheus.HistogramVec
	observabilityRows      *prometheus.HistogramVec
	observabilityBuild     *prometheus.HistogramVec
	queueDepth             *prometheus.GaugeVec
	queueBatchDepth        *prometheus.GaugeVec
	queueResidentBytes     *prometheus.GaugeVec
	queueDropped           *prometheus.CounterVec
	aggregatorBuckets      prometheus.Gauge
	aggregatorDirty        prometheus.Gauge
	aggregatorFailures     prometheus.Counter
	aggregatorPersistTime  prometheus.Gauge
	flush                  *prometheus.CounterVec
	certificateCount       *prometheus.GaugeVec
	acmeAttempts           *prometheus.CounterVec
	acmeLockConflicts      prometheus.Counter
	nodeHeartbeatAge       prometheus.Gauge
	upgrade                *prometheus.CounterVec
	trafficIntegrity       *prometheus.GaugeVec
	systemErrors           *prometheus.CounterVec
	concurrencyRejected    *prometheus.CounterVec
	tlsHandshakeRejected   *prometheus.CounterVec
	heapAlloc              prometheus.Gauge
	heapInuse              prometheus.Gauge
	heapReleased           prometheus.Gauge
	numGC                  prometheus.Gauge
	residentMemory         prometheus.Gauge
	cgroupMemoryCurrent    prometheus.Gauge
	cgroupMemoryPeak       prometheus.Gauge
	cgroupMemoryAvailable  prometheus.Gauge
	cgroupOOMEvents        prometheus.Counter
	cgroupOOMKills         prometheus.Counter
	lastDroppedTraffic     uint64
	lastDroppedHistory     uint64
	aggregatorFailureValue uint64
	lastDropReasons        map[string]map[telemetry.DropReason]uint64
	queueState             map[string]QueueSnapshot
	activeRequests         atomic.Int64
	memoryMu               sync.Mutex
	lastOOMEvents          uint64
	lastOOMKills           uint64
	oomCountersInitialized bool
}

// QueueSnapshot is the small, read-only runtime view used by the management
// dashboard. It deliberately contains no Prometheus implementation details.
type QueueSnapshot struct {
	Depth      int64
	BatchDepth int64
	Dropped    uint64
}

type RuntimeSnapshot struct {
	AsOf           int64
	HeapAllocBytes uint64
	HeapInuseBytes uint64
	ResidentBytes  uint64
	Goroutines     int
	Queues         map[string]QueueSnapshot
	ActiveRequests int64
}

func NewMetrics() *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry()}
	m.proxyRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_proxy_requests_total", Help: "Completed proxy requests."}, []string{"listener", "protocol", "method", "status_class", "status_source", "error_category"})
	m.proxyDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "yaginx_proxy_request_duration_seconds", Help: "Completed proxy request duration.", Buckets: prometheus.DefBuckets}, []string{"listener", "protocol", "method", "status_class"})
	m.proxyBytes = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_proxy_bytes_total", Help: "Proxy payload bytes."}, []string{"listener", "direction"})
	m.proxyActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_proxy_active_connections", Help: "Active proxy requests by listener and negotiated protocol."}, []string{"listener", "protocol"})
	m.configRevision = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_config_revision", Help: "Database and applied configuration revisions."}, []string{"source"})
	m.configRefresh = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_config_refresh_total", Help: "Configuration refreshes."}, []string{"mode", "result"})
	m.configRefreshDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "yaginx_config_refresh_duration_seconds", Help: "Configuration refresh duration."}, []string{"mode"})
	m.configNotifyReconnect = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_config_notify_reconnect_total", Help: "Configuration notification reconnects."}, []string{"result"})
	m.dbPoolConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_db_pool_connections", Help: "PostgreSQL pool connections."}, []string{"state"})
	m.dbOperationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "yaginx_db_operation_duration_seconds", Help: "Database operation duration."}, []string{"operation", "result"})
	m.managementDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "yaginx_management_request_duration_seconds", Help: "Management operation duration."}, []string{"operation", "result"})
	m.observabilityRows = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "yaginx_observability_rows", Help: "Rows decoded for an observability operation.", Buckets: []float64{100, 1000, 5000, 10000, 25000, 50000, 100000}}, []string{"operation", "source"})
	m.observabilityBuild = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "yaginx_observability_build_duration_seconds", Help: "Observability load and response construction duration."}, []string{"operation", "stage"})
	m.queueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_queue_depth", Help: "Asynchronous queue channel depth."}, []string{"queue"})
	m.queueBatchDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_queue_batch_depth", Help: "Events held by a queue worker batch."}, []string{"queue"})
	m.queueResidentBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_queue_resident_bytes", Help: "Approximate bytes resident in an asynchronous queue."}, []string{"queue"})
	m.queueDropped = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_queue_dropped_total", Help: "Dropped asynchronous events."}, []string{"queue", "reason"})
	m.aggregatorBuckets = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_aggregator_buckets", Help: "Active in-memory traffic aggregation buckets."})
	m.aggregatorDirty = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_aggregator_dirty_buckets", Help: "Traffic aggregation buckets awaiting persistence."})
	m.aggregatorFailures = prometheus.NewCounter(prometheus.CounterOpts{Name: "yaginx_aggregator_persistence_failures_total", Help: "Traffic aggregation persistence failures."})
	m.aggregatorPersistTime = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_aggregator_last_persist_duration_seconds", Help: "Duration of the most recent traffic aggregation persistence attempt."})
	m.flush = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_flush_total", Help: "Pipeline flush outcomes."}, []string{"pipeline", "result"})
	m.certificateCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_certificate_count", Help: "Loaded certificate count."}, []string{"source", "state", "expiry_range"})
	m.acmeAttempts = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_acme_attempts_total", Help: "ACME attempt outcomes."}, []string{"result", "error_class"})
	m.acmeLockConflicts = prometheus.NewCounter(prometheus.CounterOpts{Name: "yaginx_acme_lock_conflicts_total", Help: "ACME advisory lock conflicts."})
	m.nodeHeartbeatAge = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_node_heartbeat_age_seconds", Help: "Age of the most recent node heartbeat."})
	m.upgrade = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_upgrade_total", Help: "Manual upgrade outcomes."}, []string{"result", "stage"})
	m.trafficIntegrity = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "yaginx_traffic_statistics_integrity", Help: "Traffic aggregation integrity checks; zero is healthy."}, []string{"check"})
	m.systemErrors = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_system_errors_total", Help: "Yaginx-owned system failures."}, []string{"component", "error_code", "severity"})
	m.concurrencyRejected = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_proxy_concurrency_rejected_total", Help: "Requests rejected because the process-wide proxy concurrency limit was full."}, []string{"listener", "protocol"})
	m.tlsHandshakeRejected = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "yaginx_tls_handshake_rejected_total", Help: "TLS connections rejected because the handshake budget was full."}, []string{"listener", "reason"})
	m.heapAlloc = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_go_heap_alloc_bytes", Help: "Bytes allocated in the Go heap."})
	m.heapInuse = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_go_heap_inuse_bytes", Help: "Bytes in spans currently in use by the Go heap."})
	m.heapReleased = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_go_heap_released_bytes", Help: "Bytes released by the Go heap to the operating system."})
	m.numGC = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_go_gc_cycles_total", Help: "Number of completed Go garbage collection cycles."})
	m.residentMemory = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_process_resident_memory_bytes", Help: "Resident memory used by the Yaginx process."})
	m.cgroupMemoryCurrent = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_cgroup_memory_current_bytes", Help: "Current memory usage reported by the Yaginx cgroup."})
	m.cgroupMemoryPeak = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_cgroup_memory_peak_bytes", Help: "Peak memory usage reported by the Yaginx cgroup."})
	m.cgroupMemoryAvailable = prometheus.NewGauge(prometheus.GaugeOpts{Name: "yaginx_cgroup_memory_metrics_available", Help: "Whether cgroup memory metrics are readable; one means available."})
	m.cgroupOOMEvents = prometheus.NewCounter(prometheus.CounterOpts{Name: "yaginx_cgroup_oom_events_total", Help: "Cgroup out-of-memory events."})
	m.cgroupOOMKills = prometheus.NewCounter(prometheus.CounterOpts{Name: "yaginx_cgroup_oom_kills_total", Help: "Cgroup OOM kills."})
	m.Registry.MustRegister(m.proxyRequests, m.proxyDuration, m.proxyBytes, m.proxyActive, m.configRevision, m.configRefresh, m.configRefreshDuration, m.configNotifyReconnect, m.dbPoolConnections, m.dbOperationDuration, m.managementDuration, m.observabilityRows, m.observabilityBuild, m.queueDepth, m.queueBatchDepth, m.queueResidentBytes, m.queueDropped, m.aggregatorBuckets, m.aggregatorDirty, m.aggregatorFailures, m.aggregatorPersistTime, m.flush, m.certificateCount, m.acmeAttempts, m.acmeLockConflicts, m.nodeHeartbeatAge, m.upgrade, m.trafficIntegrity, m.systemErrors, m.concurrencyRejected, m.tlsHandshakeRejected, m.heapAlloc, m.heapInuse, m.heapReleased, m.numGC, m.residentMemory, m.cgroupMemoryCurrent, m.cgroupMemoryPeak, m.cgroupMemoryAvailable, m.cgroupOOMEvents, m.cgroupOOMKills)
	m.lastDropReasons = map[string]map[telemetry.DropReason]uint64{}
	m.queueState = map[string]QueueSnapshot{}
	// These are process-wide readiness facts. Initialize both sources so a
	// pending node has a stable, scrapeable zero rather than omitting the
	// contractual metric until its first proxy snapshot is built.
	m.SetConfigRevision(0, 0)
	return m
}

// RuntimeSnapshot returns a process-local snapshot for the management API.
func (m *Metrics) RuntimeSnapshot() RuntimeSnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	memory := readMemorySnapshot()
	m.memoryMu.Lock()
	queues := make(map[string]QueueSnapshot, len(m.queueState))
	for name, value := range m.queueState {
		queues[name] = value
	}
	m.memoryMu.Unlock()
	return RuntimeSnapshot{
		AsOf: time.Now().Unix(), HeapAllocBytes: stats.HeapAlloc, HeapInuseBytes: stats.HeapInuse,
		ResidentBytes: memory.residentBytes, Goroutines: runtime.NumGoroutine(), Queues: queues,
		ActiveRequests: m.ActiveProxyRequests(),
	}
}

func (m *Metrics) ObserveSystemError(component, code, severity string) {
	if component == "" || code == "" {
		return
	}
	switch severity {
	case "info", "warning", "error", "critical":
	default:
		severity = "error"
	}
	m.systemErrors.WithLabelValues(component, code, severity).Inc()
}

// TrackProxyRequest exposes the active in-flight request gauge at the real
// listener boundary. The returned closure is safe to defer around a request.
func (m *Metrics) TrackProxyRequest(listener, protocol string) func() {
	listener, protocol = metricListener(listener), metricProtocol(protocol)
	m.proxyActive.WithLabelValues(listener, protocol).Inc()
	m.activeRequests.Add(1)
	return func() {
		m.proxyActive.WithLabelValues(listener, protocol).Dec()
		m.activeRequests.Add(-1)
	}
}

// ActiveProxyRequests is process-wide and intentionally has no domain label.
// The management dashboard uses it for a live single-node indicator.
func (m *Metrics) ActiveProxyRequests() int64 { return max(m.activeRequests.Load(), 0) }

func (m *Metrics) ObserveConcurrentRejection(listener, protocol string) {
	m.concurrencyRejected.WithLabelValues(metricListener(listener), metricProtocol(protocol)).Inc()
}

func (m *Metrics) ObserveTLSHandshakeRejection(listener, reason string) {
	if reason == "" {
		reason = "other"
	}
	m.tlsHandshakeRejected.WithLabelValues(metricListener(listener), reason).Inc()
}

// UpdateRuntimeMemory snapshots Go heap, process RSS, and cgroup memory. The
// cgroup files are optional so the same binary remains usable outside Linux
// systemd or containers.
func (m *Metrics) UpdateRuntimeMemory() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	m.heapAlloc.Set(float64(stats.HeapAlloc))
	m.heapInuse.Set(float64(stats.HeapInuse))
	m.heapReleased.Set(float64(stats.HeapReleased))
	m.numGC.Set(float64(stats.NumGC))
	memory := readMemorySnapshot()
	m.residentMemory.Set(float64(memory.residentBytes))
	m.cgroupMemoryCurrent.Set(float64(memory.cgroupCurrentBytes))
	m.cgroupMemoryPeak.Set(float64(memory.cgroupPeakBytes))
	m.cgroupMemoryAvailable.Set(boolMetric(memory.cgroupAvailable))
	if memory.oomAvailable {
		m.memoryMu.Lock()
		if !m.oomCountersInitialized {
			m.cgroupOOMEvents.Add(float64(memory.oomEvents))
			m.cgroupOOMKills.Add(float64(memory.oomKills))
			m.oomCountersInitialized = true
		} else {
			m.cgroupOOMEvents.Add(float64(counterDelta(m.lastOOMEvents, memory.oomEvents)))
			m.cgroupOOMKills.Add(float64(counterDelta(m.lastOOMKills, memory.oomKills)))
		}
		m.lastOOMEvents = memory.oomEvents
		m.lastOOMKills = memory.oomKills
		m.memoryMu.Unlock()
	}
}

type memorySnapshot struct {
	residentBytes, cgroupCurrentBytes, cgroupPeakBytes uint64
	cgroupAvailable, oomAvailable                      bool
	oomEvents, oomKills                                uint64
}

func readMemorySnapshot() memorySnapshot {
	resident, _ := readResidentMemory()
	current, currentOK := readCgroupValue("memory.current", "memory.usage_in_bytes")
	peak, peakOK := readCgroupValue("memory.peak", "memory.max_usage_in_bytes")
	events, eventsOK := readCgroupEvent("oom")
	kills, killsOK := readCgroupEvent("oom_kill")
	return memorySnapshot{
		residentBytes: resident, cgroupCurrentBytes: current, cgroupPeakBytes: peak,
		cgroupAvailable: currentOK || peakOK, oomAvailable: eventsOK || killsOK,
		oomEvents: events, oomKills: kills,
	}
}

func readResidentMemory() (uint64, bool) {
	file, err := os.Open(procStatusPath)
	if err != nil {
		return 0, false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				return value * 1024, true
			}
		}
	}
	return 0, false
}

var (
	procStatusPath = "/proc/self/status"
	procCgroupPath = "/proc/self/cgroup"
	cgroupRoot     = "/sys/fs/cgroup"
)

func cgroupMemoryBases() []string {
	raw, err := os.ReadFile(procCgroupPath)
	if err != nil {
		return []string{cgroupRoot}
	}
	bases := []string{cgroupRoot}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		switch {
		case parts[0] == "0":
			bases = append(bases, filepath.Join(cgroupRoot, parts[2]))
		case strings.Contains(parts[1], "memory"):
			bases = append(bases, filepath.Join(cgroupRoot, "memory", parts[2]))
		}
	}
	return uniqueStrings(bases)
}

func readCgroupValue(names ...string) (uint64, bool) {
	for _, base := range cgroupMemoryBases() {
		for _, name := range names {
			value, err := os.ReadFile(filepath.Join(base, name))
			if err != nil {
				continue
			}
			parsed, err := strconv.ParseUint(strings.TrimSpace(string(value)), 10, 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func readCgroupEvent(name string) (uint64, bool) {
	for _, base := range cgroupMemoryBases() {
		value, err := os.ReadFile(filepath.Join(base, "memory.events"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(value), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != name {
				continue
			}
			parsed, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func counterDelta(previous, current uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (m *Metrics) ObserveProxy(event telemetry.Event) {
	status := event.HistoryStatus
	if status == 0 {
		status = event.Status
	}
	method, protocol, class := metricMethod(event.Method), metricProtocol(event.Protocol), metricStatusClass(status)
	source, category := metricSource(event.StatusSource), metricCategory(event.ErrorCategory)
	m.proxyRequests.WithLabelValues("business", protocol, method, class, source, category).Inc()
	m.proxyDuration.WithLabelValues("business", protocol, method, class).Observe(float64(event.DurationMS) / 1000)
	m.proxyBytes.WithLabelValues("business", "inbound").Add(float64(max(event.Inbound, 0)))
	m.proxyBytes.WithLabelValues("business", "outbound").Add(float64(max(event.Outbound, 0)))
}

func (m *Metrics) SetConfigRevision(database, applied int64) {
	m.configRevision.WithLabelValues("database").Set(float64(database))
	m.configRevision.WithLabelValues("applied").Set(float64(applied))
}

func (m *Metrics) ObserveConfigRefresh(mode, result string, seconds float64) {
	m.configRefresh.WithLabelValues(metricMode(mode), metricResult(result)).Inc()
	m.configRefreshDuration.WithLabelValues(metricMode(mode)).Observe(seconds)
}

func (m *Metrics) ObserveConfigNotifyReconnect(result string) {
	m.configNotifyReconnect.WithLabelValues(metricResult(result)).Inc()
}

func (m *Metrics) ObserveDatabaseOperation(operation, result string, duration time.Duration) {
	if operation != "health" {
		operation = "other"
	}
	m.dbOperationDuration.WithLabelValues(operation, metricResult(result)).Observe(duration.Seconds())
}

func (m *Metrics) ObserveManagementRequest(operation, result string, duration time.Duration) {
	m.managementDuration.WithLabelValues(metricObservabilityOperation(operation), metricResult(result)).Observe(duration.Seconds())
}

func (m *Metrics) ObserveObservabilityLoad(operation, source string, rows int, loadDuration, buildDuration time.Duration) {
	operation = metricObservabilityOperation(operation)
	switch source {
	case "gateway_traffic_contribution", "resource_report", "gateway_traffic_contribution+resource_report":
	default:
		source = "other"
	}
	m.observabilityRows.WithLabelValues(operation, source).Observe(float64(max(rows, 0)))
	m.observabilityBuild.WithLabelValues(operation, "load").Observe(loadDuration.Seconds())
	m.observabilityBuild.WithLabelValues(operation, "build").Observe(buildDuration.Seconds())
}

func (m *Metrics) ObserveFlush(pipeline, result string) {
	m.flush.WithLabelValues(metricPipeline(pipeline), metricResult(result)).Inc()
}

func (m *Metrics) UpdatePool(acquired, idle, constructing, maximum int32) {
	m.dbPoolConnections.WithLabelValues("acquired").Set(float64(acquired))
	m.dbPoolConnections.WithLabelValues("idle").Set(float64(idle))
	m.dbPoolConnections.WithLabelValues("constructing").Set(float64(constructing))
	m.dbPoolConnections.WithLabelValues("max").Set(float64(maximum))
}

func (m *Metrics) UpdateQueue(queue string, depth int, dropped uint64) {
	m.UpdateQueueMetrics(queue, depth, 0, 0, dropped, nil)
}

func (m *Metrics) UpdateAggregator(stats telemetry.AggregatorStats) {
	m.aggregatorBuckets.Set(float64(max(stats.Buckets, 0)))
	m.aggregatorDirty.Set(float64(max(stats.DirtyBuckets, 0)))
	m.aggregatorFailures.Add(float64(stats.PersistenceFailures - m.aggregatorFailureValue))
	m.aggregatorFailureValue = stats.PersistenceFailures
	m.aggregatorPersistTime.Set(stats.LastPersistDuration.Seconds())
}

func (m *Metrics) UpdateQueueMetrics(queue string, depth, batchDepth int, residentBytes int64, dropped uint64, reasons map[telemetry.DropReason]uint64) {
	queue = metricQueue(queue)
	m.memoryMu.Lock()
	m.queueState[queue] = QueueSnapshot{Depth: int64(max(depth, 0)), BatchDepth: int64(max(batchDepth, 0)), Dropped: dropped}
	m.memoryMu.Unlock()
	m.queueDepth.WithLabelValues(queue).Set(float64(max(depth, 0)))
	m.queueBatchDepth.WithLabelValues(queue).Set(float64(max(batchDepth, 0)))
	if residentBytes < 0 {
		residentBytes = 0
	}
	m.queueResidentBytes.WithLabelValues(queue).Set(float64(residentBytes))
	var previous *uint64
	if queue == "traffic" {
		previous = &m.lastDroppedTraffic
	} else {
		previous = &m.lastDroppedHistory
	}
	if dropped > *previous {
		m.queueDropped.WithLabelValues(queue, "total").Add(float64(dropped - *previous))
		*previous = dropped
	}
	if _, ok := m.lastDropReasons[queue]; !ok {
		m.lastDropReasons[queue] = map[telemetry.DropReason]uint64{}
	}
	for reason, value := range reasons {
		previous := m.lastDropReasons[queue][reason]
		if value > previous {
			m.queueDropped.WithLabelValues(queue, string(reason)).Add(float64(value - previous))
			m.lastDropReasons[queue][reason] = value
		}
	}
}

func (m *Metrics) SetCertificateCount(source, state, expiryRange string, value int) {
	if source != "database" && source != "lkg" {
		source = "other"
	}
	if state != "active" && state != "inactive" {
		state = "other"
	}
	switch expiryRange {
	case "expired", "under_7d", "under_35d", "over_35d":
	default:
		expiryRange = "other"
	}
	m.certificateCount.WithLabelValues(source, state, expiryRange).Set(float64(max(value, 0)))
}

func (m *Metrics) ObserveACMEAttempt(result, errorClass string) {
	if result != "success" && result != "failure" && result != "skipped" {
		result = "failure"
	}
	switch errorClass {
	case "none", "permanent", "temporary", "lock", "invalid", "other":
	default:
		errorClass = "other"
	}
	m.acmeAttempts.WithLabelValues(result, errorClass).Inc()
}

func (m *Metrics) ObserveACMELockConflict() { m.acmeLockConflicts.Inc() }

func (m *Metrics) SetNodeHeartbeatAge(age time.Duration) {
	if age < 0 {
		age = 0
	}
	m.nodeHeartbeatAge.Set(age.Seconds())
}

func (m *Metrics) SetTrafficIntegrity(check string, value int64) {
	switch check {
	case "contribution_report_mismatch", "missing_report_bucket", "status_nonconservation", "latency_incomplete", "ua_incomplete", "materialization_lag_seconds":
	default:
		check = "contribution_report_mismatch"
	}
	m.trafficIntegrity.WithLabelValues(check).Set(float64(max(value, 0)))
}

type LabelPolicy struct {
	Allowed  map[string]struct{}
	Fallback string
}

func sanitizeLabel(value string, policy LabelPolicy) string {
	if _, ok := policy.Allowed[value]; ok {
		return value
	}
	return policy.Fallback
}

func labelPolicy(values ...string) LabelPolicy {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	return LabelPolicy{Allowed: allowed, Fallback: "other"}
}

var (
	methodLabels        = labelPolicy("GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
	listenerLabels      = labelPolicy("http", "proxy_http", "https", "proxy_https", "http3")
	protocolLabels      = labelPolicy("HTTP/1.1", "HTTP/2.0", "HTTP/3.0")
	sourceLabels        = labelPolicy("Upstream", "YaginxProxy", "YaginxPipeline")
	pipelineLabels      = labelPolicy("traffic", "history", "report")
	observabilityLabels = labelPolicy("overview", "domains", "timeseries", "contributions", "details", "dashboard", "catalog", "subject", "database")
)

func init() {
	listenerLabels.Fallback = "business"
	pipelineLabels.Fallback = "traffic"
}

func metricMethod(value string) string {
	return strings.ToUpper(sanitizeLabel(value, methodLabels))
}
func metricListener(value string) string {
	return sanitizeLabel(value, listenerLabels)
}
func metricProtocol(value string) string {
	return sanitizeLabel(value, protocolLabels)
}
func metricStatusClass(status int) string {
	if status < 100 || status > 699 {
		return "none"
	}
	return strconv.Itoa(status/100) + "xx"
}
func metricSource(value string) string {
	return sanitizeLabel(value, sourceLabels)
}
func metricCategory(value string) string {
	switch value {
	case "", "NoAvailableDestination", "UpstreamReturnedError", "UpstreamRequestFailure", "SiteMaintenance":
		return map[bool]string{true: "none", false: value}[value == ""]
	}
	return "other"
}
func metricMode(value string) string {
	if value == "full" {
		return "full"
	}
	return "incremental"
}
func metricResult(value string) string {
	if value == "success" {
		return "success"
	}
	return "failure"
}
func metricPipeline(value string) string {
	return sanitizeLabel(value, pipelineLabels)
}
func metricQueue(value string) string {
	if value == "history" {
		return "history"
	}
	return "traffic"
}

func metricObservabilityOperation(value string) string {
	return sanitizeLabel(value, observabilityLabels)
}
