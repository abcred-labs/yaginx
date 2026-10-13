package observability

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yaginx-gateway/internal/telemetry"
)

func TestProxyMetricsUseBoundedLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveProxy(telemetry.Event{Method: "TRACE", Protocol: "HTTP/9", HistoryStatus: 604, StatusSource: "untrusted-source", ErrorCategory: "host.example.invalid/secret", DurationMS: 50, Inbound: 3, Outbound: 5, At: time.Now()})
	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range values {
		if family.GetName() != "yaginx_proxy_requests_total" {
			continue
		}
		if len(family.Metric) != 1 {
			t.Fatalf("metrics=%d", len(family.Metric))
		}
		labels := map[string]string{}
		for _, pair := range family.Metric[0].Label {
			labels[pair.GetName()] = pair.GetValue()
		}
		if labels["method"] != "OTHER" || labels["protocol"] != "other" || labels["status_class"] != "6xx" || labels["status_source"] != "other" || labels["error_category"] != "other" {
			t.Fatalf("labels=%#v", labels)
		}
		return
	}
	t.Fatal("proxy request metric was not gathered")
}

func TestProxyMetricsKeepSiteMaintenanceCategory(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveProxy(telemetry.Event{Method: http.MethodGet, Protocol: "HTTP/1.1", HistoryStatus: http.StatusServiceUnavailable, StatusSource: "YaginxPipeline", ErrorCategory: "SiteMaintenance", At: time.Now()})
	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range values {
		if family.GetName() != "yaginx_proxy_requests_total" {
			continue
		}
		labels := map[string]string{}
		for _, pair := range family.Metric[0].Label {
			labels[pair.GetName()] = pair.GetValue()
		}
		if labels["status_class"] != "5xx" || labels["status_source"] != "YaginxPipeline" || labels["error_category"] != "SiteMaintenance" {
			t.Fatalf("labels=%#v", labels)
		}
		return
	}
	t.Fatal("proxy request metric was not gathered")
}

func TestRuntimeProducerMetricsAreGatheredWithBoundedLabels(t *testing.T) {
	metrics := NewMetrics()
	done := metrics.TrackProxyRequest("unexpected-listener", "HTTP/9")
	done()
	metrics.ObserveConcurrentRejection("http", "HTTP/1.1")
	metrics.ObserveTLSHandshakeRejection("https", "limit")
	metrics.ObserveConfigNotifyReconnect("unexpected")
	metrics.ObserveDatabaseOperation("unexpected", "unexpected", time.Millisecond)
	metrics.SetCertificateCount("unexpected", "unexpected", "unexpected", 1)
	metrics.ObserveACMEAttempt("unexpected", "unexpected")
	metrics.ObserveACMELockConflict()
	metrics.SetNodeHeartbeatAge(time.Second)
	metrics.ObserveManagementRequest("not-a-route", "unexpected", time.Millisecond)
	metrics.ObserveObservabilityLoad("not-a-route", "host.example.invalid", 8, time.Millisecond, time.Millisecond)
	metrics.UpdateRuntimeMemory()
	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"yaginx_proxy_active_connections":             false,
		"yaginx_proxy_concurrency_rejected_total":     false,
		"yaginx_tls_handshake_rejected_total":         false,
		"yaginx_config_notify_reconnect_total":        false,
		"yaginx_db_operation_duration_seconds":        false,
		"yaginx_certificate_count":                    false,
		"yaginx_acme_attempts_total":                  false,
		"yaginx_acme_lock_conflicts_total":            false,
		"yaginx_node_heartbeat_age_seconds":           false,
		"yaginx_management_request_duration_seconds":  false,
		"yaginx_observability_rows":                   false,
		"yaginx_observability_build_duration_seconds": false,
		"yaginx_go_heap_alloc_bytes":                  false,
		"yaginx_go_heap_inuse_bytes":                  false,
		"yaginx_go_heap_released_bytes":               false,
		"yaginx_go_gc_cycles_total":                   false,
		"yaginx_process_resident_memory_bytes":        false,
		"yaginx_cgroup_memory_current_bytes":          false,
		"yaginx_cgroup_memory_peak_bytes":             false,
		"yaginx_cgroup_memory_metrics_available":      false,
		"yaginx_cgroup_oom_events_total":              false,
		"yaginx_cgroup_oom_kills_total":               false,
	}
	for _, family := range values {
		if family.GetName() == "yaginx_management_request_duration_seconds" {
			for _, metric := range family.Metric {
				labels := map[string]string{}
				for _, pair := range metric.Label {
					labels[pair.GetName()] = pair.GetValue()
				}
				if labels["operation"] != "other" || labels["result"] != "failure" {
					t.Fatalf("management labels=%#v", labels)
				}
			}
		}
		if family.GetName() == "yaginx_observability_rows" {
			for _, metric := range family.Metric {
				labels := map[string]string{}
				for _, pair := range metric.Label {
					labels[pair.GetName()] = pair.GetValue()
				}
				if labels["operation"] != "other" || labels["source"] != "other" {
					t.Fatalf("observability labels=%#v", labels)
				}
			}
		}
		if _, ok := want[family.GetName()]; ok {
			want[family.GetName()] = true
		}
	}
	for name, gathered := range want {
		if !gathered {
			t.Fatalf("metric %s was not gathered", name)
		}
	}
}

func TestObservabilityMetricsKeepDashboardOperationLabel(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveObservabilityLoad("dashboard", "gateway_traffic_contribution", 3, time.Millisecond, 2*time.Millisecond)
	metrics.ObserveObservabilityLoad("unknown", "gateway_traffic_contribution", 1, time.Millisecond, time.Millisecond)

	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	rowsLabels := map[string]bool{}
	buildLabels := map[string]bool{}
	for _, family := range values {
		switch family.GetName() {
		case "yaginx_observability_rows":
			for _, metric := range family.Metric {
				labels := map[string]string{}
				for _, pair := range metric.Label {
					labels[pair.GetName()] = pair.GetValue()
				}
				switch labels["operation"] {
				case "dashboard":
					if labels["source"] != "gateway_traffic_contribution" {
						t.Fatalf("dashboard labels=%#v", labels)
					}
				case "other":
					if labels["source"] != "gateway_traffic_contribution" {
						t.Fatalf("unknown labels=%#v", labels)
					}
				default:
					t.Fatalf("unexpected observability labels=%#v", labels)
				}
				rowsLabels[labels["operation"]] = true
			}
		case "yaginx_observability_build_duration_seconds":
			for _, metric := range family.Metric {
				labels := map[string]string{}
				for _, pair := range metric.Label {
					labels[pair.GetName()] = pair.GetValue()
				}
				if labels["operation"] != "dashboard" && labels["operation"] != "other" {
					t.Fatalf("unexpected build labels=%#v", labels)
				}
				buildLabels[labels["operation"]] = true
			}
		}
	}
	if !rowsLabels["dashboard"] || !rowsLabels["other"] {
		t.Fatalf("observability rows labels=%v", rowsLabels)
	}
	if !buildLabels["dashboard"] || !buildLabels["other"] {
		t.Fatalf("observability build labels=%v", buildLabels)
	}
}

func TestMemorySnapshotReadsCgroupV2AndReportsAvailability(t *testing.T) {
	root := t.TempDir()
	cgroupPath := filepath.Join(root, "service")
	if err := os.MkdirAll(cgroupPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cgroupPath, "memory.current"), []byte("1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cgroupPath, "memory.peak"), []byte("5678\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cgroupPath, "memory.events"), []byte("oom 3\noom_kill 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(root, "cgroup")
	if err := os.WriteFile(proc, []byte("0::/service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldProc, oldRoot := procCgroupPath, cgroupRoot
	procCgroupPath, cgroupRoot = proc, root
	t.Cleanup(func() {
		procCgroupPath, cgroupRoot = oldProc, oldRoot
	})

	snapshot := readMemorySnapshot()
	if !snapshot.cgroupAvailable || !snapshot.oomAvailable || snapshot.cgroupCurrentBytes != 1234 || snapshot.cgroupPeakBytes != 5678 || snapshot.oomEvents != 3 || snapshot.oomKills != 2 {
		t.Fatalf("memory snapshot=%#v", snapshot)
	}
	metrics := NewMetrics()
	metrics.UpdateRuntimeMemory()
	if err := os.WriteFile(filepath.Join(cgroupPath, "memory.events"), []byte("oom 5\noom_kill 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics.UpdateRuntimeMemory()
	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range values {
		switch family.GetName() {
		case "yaginx_cgroup_oom_events_total":
			if len(family.Metric) != 1 || family.Metric[0].GetCounter().GetValue() != 5 {
				t.Fatalf("oom events metric=%#v", family)
			}
		case "yaginx_cgroup_oom_kills_total":
			if len(family.Metric) != 1 || family.Metric[0].GetCounter().GetValue() != 4 {
				t.Fatalf("oom kills metric=%#v", family)
			}
		}
	}
}

func TestMemoryMetricsExposeOOMCountersAsCounters(t *testing.T) {
	metrics := NewMetrics()
	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range values {
		switch family.GetName() {
		case "yaginx_cgroup_oom_events_total", "yaginx_cgroup_oom_kills_total":
			if family.GetType().String() != "COUNTER" {
				t.Fatalf("%s type=%s, want COUNTER", family.GetName(), family.GetType())
			}
		}
	}
}
