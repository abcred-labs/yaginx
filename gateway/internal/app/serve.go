package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
	"yaginx-gateway/internal/acme"
	"yaginx-gateway/internal/analytics"
	"yaginx-gateway/internal/auth"
	"yaginx-gateway/internal/bootstrap"
	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/configsync"
	"yaginx-gateway/internal/core"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/listeners"
	"yaginx-gateway/internal/management"
	managementserver "yaginx-gateway/internal/management/server"
	"yaginx-gateway/internal/nodes"
	"yaginx-gateway/internal/observability"
	"yaginx-gateway/internal/platform/identity"
	"yaginx-gateway/internal/platform/snowflake"
	"yaginx-gateway/internal/proxy"
	"yaginx-gateway/internal/repository"
	"yaginx-gateway/internal/retention"
	"yaginx-gateway/internal/sysevent"
	"yaginx-gateway/internal/telemetry"
)

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	validateOnly := fs.Bool("validate", false, "validate runtime configuration and database material without starting listeners")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.LoadRuntime(*path)
	if err != nil {
		return runSetup(ctx, *path, fmt.Sprintf("runtime configuration is missing or invalid: %v", err), stdout, stderr)
	}
	checkContext, checkCancel := context.WithTimeout(ctx, 10*time.Second)
	if err := database.Check(checkContext, cfg.DatabaseDSN(), cfg.Int("database", "minimumServerMajor")); err != nil {
		checkCancel()
		return runSetup(ctx, *path, fmt.Sprintf("database is not ready; check the connection details: %v", err), stdout, stderr)
	}
	needsMigration, err := database.DatabaseNeedsMigration(checkContext, cfg.DatabaseDSN())
	checkCancel()
	if err != nil {
		return runSetup(ctx, *path, fmt.Sprintf("cannot determine database schema state: %v", err), stdout, stderr)
	}
	if needsMigration {
		return runSetup(ctx, *path, fmt.Sprintf("DATABASE MIGRATION REQUIRED: the installed version needs a newer database schema; run `yaginx database migrate --config %s` and restart the service (or use the setup page)", *path), stdout, stderr)
	}
	for _, warning := range cfg.Warnings {
		fmt.Fprintf(stderr, "configuration warning: %s\n", warning)
	}
	var acmeWorker *acme.Worker
	logger := bootstrap.NewLogger(stdout, cfg.String("observability", "logLevel"), cfg.String("observability", "logFormat"))
	identityValue, err := identity.LoadOrCreate(cfg.String("node", "identityFile"))
	if err != nil {
		fmt.Fprintf(stderr, "node identity: %v\n", err)
		return 1
	}
	poolOptions, err := runtimePoolOptions(cfg)
	if err != nil {
		return failServe(stderr, err)
	}
	pool, err := database.NewRuntimePool(ctx, cfg.DatabaseDSN(), poolOptions)
	if err != nil {
		fmt.Fprintf(stderr, "database pool: %v\n", err)
		return 1
	}
	defer pool.Close()
	store := repository.New(pool)
	store.SetRuntimeIdentity(identityValue.NodeID, Version, Commit)
	if err := store.EnsureSystemErrorSchema(ctx); err != nil {
		fmt.Fprintf(stderr, "system error schema: %v\n", err)
		return 1
	}
	runtimeSetting, err := store.GetRuntimeSetting(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "load shared runtime settings: %v\n", err)
		return 1
	}
	if runtimeSetting == nil {
		shared, extractErr := config.ExtractSharedRuntime(cfg.Values)
		if extractErr != nil {
			fmt.Fprintf(stderr, "extract shared runtime settings: %v\n", extractErr)
			return 1
		}
		runtimeSetting, err = store.SetRuntimeSetting(ctx, management.RuntimeSettingUpsertRequest{Config: shared})
		if err != nil {
			fmt.Fprintf(stderr, "initialize shared runtime settings: %v\n", err)
			return 1
		}
		if err := config.MigrateFileToBootstrap(*path, cfg.Values); err != nil {
			fmt.Fprintf(stderr, "migrate local configuration to bootstrap: %v\n", err)
			return 1
		}
	}
	if runtimeSetting != nil {
		for _, warning := range runtimeSetting.Warnings {
			fmt.Fprintf(stderr, "configuration warning: %s\n", warning)
		}
		if err := config.ApplySharedRuntime(runtimeSetting.Config, cfg.Values); err != nil {
			fmt.Fprintf(stderr, "validate shared runtime settings: %v\n", err)
			return 1
		}
	}
	// Persist the migrated bootstrap shape after shared settings have been
	// resolved. This is idempotent and keeps legacy fields out of future boots.
	if err := config.MigrateFileToBootstrap(*path, cfg.Values); err != nil {
		fmt.Fprintf(stderr, "migrate local configuration to bootstrap: %v\n", err)
		return 1
	}
	// Parsed only after ApplySharedRuntime, so the shared settings held in the
	// database govern these tunables rather than whatever the local file happens
	// to carry.
	settings, err := parseServeSettings(cfg)
	if err != nil {
		return failServe(stderr, err)
	}
	proxyHeaderTimeout := settings.ProxyHeaderTimeout
	telemetryFlushInterval := settings.TelemetryFlushInterval
	historyFlushInterval := settings.HistoryFlushInterval
	monitorInfoRetention := settings.MonitorInfoRetention
	contributionRetention := settings.ContributionRetention
	resourceMinutelyDelay := settings.ResourceMinutelyDelay
	resourceHourlyDelay := settings.ResourceHourlyDelay
	resourceDailyDelay := settings.ResourceDailyDelay
	resourceReconciliationLookback := settings.ResourceReconciliationLookback
	resourceMinutelyReconciliationLookback := settings.ResourceMinutelyReconciliationLookback
	resourceMaterializationStatementTimeout := settings.ResourceMaterializationStatementTimeout
	configEventRetention := settings.ConfigEventRetention
	inactiveNodeAfter := settings.InactiveNodeAfter
	incrementalBatchSize := settings.IncrementalBatchSize
	revisionPollInterval := settings.RevisionPollInterval
	notifyDebounce := settings.NotifyDebounce
	reconnectInitialBackoff := settings.ReconnectInitialBackoff
	reconnectMaximumBackoff := settings.ReconnectMaximumBackoff
	shutdownGracePeriod := settings.ShutdownGracePeriod
	heartbeatInterval := settings.HeartbeatInterval
	heartbeatStaleAfter := settings.HeartbeatStaleAfter
	drainTimeout := settings.DrainTimeout
	store.SetContributionRetention(contributionRetention)
	// The metrics registry does not exist yet, and the periodic maintenance
	// workers deliberately journal their failures without counting them, so the
	// quiet recorder stands in for both cases.
	quietEvents := sysevent.StoreRecorder{Store: store, Logger: logger}
	runtimeMaterial, err := store.LoadOrCreateRuntimeMaterial(ctx)
	if err != nil {
		quietEvents.Record(ctx, "database", "runtime_material", "critical", "RUNTIME_MATERIAL_LOAD_FAILED", err)
		fmt.Fprintf(stderr, "load runtime material: %v\n", err)
		return 1
	}
	if *validateOnly {
		fmt.Fprintln(stdout, "runtime configuration and database material: ok")
		return 0
	}
	lifecycle, cancel := bootstrap.ContextWithSignals(ctx)
	defer cancel()
	otlpExportTimeout, err := cfg.Duration("observability.otlp", "exportTimeout")
	if err != nil {
		return failServe(stderr, err)
	}
	tracing, err := observability.NewTracing(lifecycle, observability.OTLPSettings{
		Enabled:       cfg.Bool("observability.otlp", "enabled"),
		Endpoint:      cfg.String("observability.otlp", "endpoint"),
		Protocol:      cfg.String("observability.otlp", "protocol"),
		Insecure:      cfg.Bool("observability.otlp", "insecure"),
		SampleRatio:   cfg.Float("observability.otlp", "sampleRatio"),
		ExportTimeout: otlpExportTimeout,
	}, Version, Commit)
	if err != nil {
		return failServe(stderr, err)
	}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), otlpExportTimeout)
		defer stop()
		if err := tracing.Shutdown(shutdown); err != nil {
			logger.Warn("flush OTLP tracing", "error", err)
		}
	}()
	adminAddress, err := cfg.ManagementAddress()
	if err != nil {
		fmt.Fprintf(stderr, "management listener: %v\n", err)
		return 1
	}
	keys := auth.Keys{Current: runtimeMaterial.JWTCurrent, Previous: runtimeMaterial.JWTPrevious, Issuer: cfg.String("security", "jwtIssuer"), Audience: cfg.String("security", "jwtAudience"), Session: time.Duration(cfg.Int("security", "sessionSeconds")) * time.Second}
	var activeKeys atomic.Value
	activeKeys.Store(keys)
	store.SetNodeHeartbeatStaleAfter(heartbeatStaleAfter)
	store.SetInactiveNodeAfter(inactiveNodeAfter)
	store.SetTrafficMaterializerLockNamespace(cfg.String("resourceReports", "leaderLockNamespace"))
	ipProfileTimeout, err := cfg.Duration("ipLookup", "timeout")
	if err != nil {
		return failServe(stderr, err)
	}
	store.SetIPProfileTimeout(ipProfileTimeout)
	fingerprint, err := identity.MachineFingerprint()
	if err != nil {
		fmt.Fprintf(stderr, "machine fingerprint: %v\n", err)
		return 1
	}
	displayName, _ := os.Hostname()
	registered, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{
		NodeID:             identityValue.NodeID,
		MachineFingerprint: fingerprint,
		DisplayName:        displayName,
		SoftwareVersion:    Version,
		GitSHA:             Commit,
	})
	if err != nil {
		fmt.Fprintf(stderr, "register node: %v\n", err)
		return 1
	}
	nodeStatus := registered.Status
	health := newHealthState(Version, Commit, identityValue.NodeID, nodeStatus)
	metrics := observability.NewMetrics()
	events := sysevent.StoreRecorder{Store: store, Logger: logger, Metrics: metrics}
	store.SetObservabilityObserver(metrics.ObserveObservabilityLoad)
	api := managementserver.New(managementserver.Options{
		Store: store, SystemErrors: store, KeysSource: func() auth.Keys { return activeKeys.Load().(auth.Keys) }, BodyMax: int64(cfg.Int("admin", "apiBodyMaxBytes")),
		ActiveProxyRequests: metrics.ActiveProxyRequests, RuntimeSnapshot: func() management.GatewayRuntimeSnapshot {
			snapshot := metrics.RuntimeSnapshot()
			queues := make(map[string]management.GatewayRuntimeQueue, len(snapshot.Queues))
			for name, queue := range snapshot.Queues {
				queues[name] = management.GatewayRuntimeQueue{Depth: queue.Depth, BatchDepth: queue.BatchDepth, Dropped: queue.Dropped}
			}
			return management.GatewayRuntimeSnapshot{AsOf: snapshot.AsOf, HeapAllocBytes: snapshot.HeapAllocBytes, HeapInuseBytes: snapshot.HeapInuseBytes, ResidentBytes: snapshot.ResidentBytes, Goroutines: snapshot.Goroutines, ActiveRequests: snapshot.ActiveRequests, Queues: queues}
		}, ObserveManagementRequest: metrics.ObserveManagementRequest, Logger: logger,
		SessionIdleTimeout:      time.Duration(cfg.Int("security", "idleTimeoutSeconds")) * time.Second,
		SessionAbsoluteTimeout:  time.Duration(cfg.Int("security", "absoluteSeconds")) * time.Second,
		SessionRememberIdle:     time.Duration(cfg.Int("security", "rememberIdleSeconds")) * time.Second,
		SessionRememberAbsolute: time.Duration(cfg.Int("security", "rememberAbsoluteSeconds")) * time.Second,
		RecordSystemError: func(ctx context.Context, component, operation, severity, code string, err error) {
			events.Record(ctx, component, operation, severity, code, err)
		},
		ACME: acmeTrigger{store: store, worker: func() *acme.Worker { return acmeWorker }},
	})
	databaseHealthTimeout, err := cfg.Duration("database", "healthQueryTimeout")
	if err != nil {
		return failServe(stderr, err)
	}
	api = management.DatabaseGuard(api, func(request context.Context) error {
		check, cancel := context.WithTimeout(request, databaseHealthTimeout)
		defer cancel()
		return pool.Ping(check)
	})
	admin := http.NewServeMux()
	admin.HandleFunc(cfg.String("observability", "livePath"), health.live)
	admin.HandleFunc(cfg.String("observability", "readyPath"), health.readyHandler)
	admin.HandleFunc("/healthz", health.live)
	admin.HandleFunc("/readyz", health.readyHandler)
	admin.Handle(cfg.String("observability", "metricsPath"), promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))
	{
		keyValue := management.KeyValueHandler(store)
		keyValue = management.DatabaseGuard(keyValue, func(request context.Context) error {
			check, cancel := context.WithTimeout(request, databaseHealthTimeout)
			defer cancel()
			return pool.Ping(check)
		})
		admin.Handle("/api/key_value/", keyValue)
	}
	clusterID := identity.ClusterID(cfg.DatabaseDSN())
	admin.HandleFunc("/api/identity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"service": "yaginx", "clusterId": clusterID, "nodeId": identityValue.NodeID, "version": 1})
	})
	admin.Handle("/api/", api)
	admin.Handle("/mcp", management.MCPHandler(store, func(requestContext context.Context, candidate string) bool {
		return store.ValidateAPIToken(requestContext, candidate)
	}))
	admin.HandleFunc("/.well-known/mcp.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(map[string]any{"mcpServers": map[string]any{"yaginx": map[string]string{"url": scheme + "://" + r.Host + "/mcp"}}})
	})
	registerLoopbackProfiles(admin)
	admin.HandleFunc("/yaginx_console/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	uiPath := cfg.String("admin", "uiDirectory")
	admin.Handle("/", spaStaticWithCache(uiPath, cfg.String("admin", "indexCacheControl"), cfg.String("admin", "hashedAssetCacheControl")))
	guardedAdmin, err := managementCIDRGuardWithLogger(cfg.Strings("management", "allowedCIDRs"), admin, logger)
	if err != nil {
		return failServe(stderr, fmt.Errorf("management.allowedCIDRs: %w", err))
	}
	logger.Info("management access policy loaded", "allowedCIDRs", cfg.Strings("management", "allowedCIDRs"), "source", "local-config")
	guardedAdmin = tracing.Handler("management", guardedAdmin)
	adminServer := &http.Server{Addr: adminAddress, Handler: guardedAdmin, ReadHeaderTimeout: 30 * time.Second, IdleTimeout: time.Minute}
	go func() {
		if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("management listener stopped", "error", err)
		}
	}()
	health.setNodeStatus(registered.Status)
	nodeNumber, err := store.NodeNumber(lifecycle, identityValue.NodeID)
	if err != nil {
		_ = adminServer.Shutdown(context.Background())
		return failServe(stderr, fmt.Errorf("active node number: %w", err))
	}
	idGenerator, err := snowflake.LoadOrCreate(nodeNumber, filepath.Join(filepath.Dir(cfg.String("node", "identityFile")), "snowflake.state"))
	if err != nil {
		_ = adminServer.Shutdown(context.Background())
		return failServe(stderr, fmt.Errorf("snowflake: %w", err))
	}
	store.SetRequestIDGenerator(idGenerator.Next)
	go idGenerator.RunPersistence(lifecycle.Done())
	httpAddress, err := cfg.ListenerAddress("http")
	if err != nil {
		return failServe(stderr, err)
	}
	proxyHTTPAddress, err := cfg.ListenerAddress("proxyHttp")
	if err != nil {
		return failServe(stderr, err)
	}
	httpsAddress, err := cfg.ListenerAddress("https")
	if err != nil {
		return failServe(stderr, err)
	}
	proxyHTTPSAddress, err := cfg.ListenerAddress("proxyHttps")
	if err != nil {
		return failServe(stderr, err)
	}
	h3Address, err := cfg.HTTP3Address()
	if err != nil {
		return failServe(stderr, err)
	}
	h3IdleTimeout, err := cfg.Duration("listeners.http3", "maxIdleTimeout")
	if err != nil {
		return failServe(stderr, err)
	}
	proxySettings, err := runtimeProxySettings(cfg)
	if err != nil {
		return failServe(stderr, err)
	}
	proxySettings.BlacklistCandidateWarning = func(candidate, remote, host string) {
		logger.Warn("using first X-Forwarded-For value for IP blacklist compatibility", "candidate", candidate, "remote", remote, "host", host)
	}
	coreListeners := core.ListenerPlan{
		HTTP: httpAddress, ProxyHTTP: proxyHTTPAddress, HTTPS: httpsAddress, ProxyHTTPS: proxyHTTPSAddress, HTTP3: h3Address,
		TrustedCIDRs: cfg.Strings("listeners.proxyProtocol", "trustedCIDRs"), RequireProxyProtocol: cfg.Bool("listeners.proxyProtocol", "requiredOnProxyListeners"),
		HeaderTimeout: proxyHeaderTimeout.String(), HTTP3MaxIdleTimeout: h3IdleTimeout.String(),
	}
	certificatesManager := certificates.NewManager()
	certificateEntries, err := store.LoadCertificates(ctx)
	if err != nil {
		_ = adminServer.Shutdown(context.Background())
		events.Record(ctx, "certificate", "load", "error", "CERTIFICATE_LOAD_FAILED", err)
		fmt.Fprintf(stderr, "load certificates: %v\n", err)
		return 1
	}
	if err := certificatesManager.Replace(certificateEntries); err != nil {
		_ = adminServer.Shutdown(context.Background())
		events.Record(ctx, "certificate", "activate", "error", "CERTIFICATE_ACTIVATION_FAILED", err)
		fmt.Fprintf(stderr, "activate certificates: %v\n", err)
		return 1
	}
	setCertificateMetrics(metrics, "database", certificateEntries)
	snapshot, err := store.LoadProxySnapshot(ctx)
	if err != nil {
		_ = adminServer.Shutdown(context.Background())
		events.Record(ctx, "configsync", "load_snapshot", "critical", "PROXY_SNAPSHOT_LOAD_FAILED", err)
		fmt.Fprintf(stderr, "load proxy snapshot: %v\n", err)
		return 1
	}
	business, err := proxy.NewConfigured(snapshot, proxySettings)
	if err != nil {
		_ = adminServer.Shutdown(context.Background())
		events.Record(ctx, "runtime", "configure_proxy", "critical", "PROXY_RUNTIME_CONFIGURATION_FAILED", err)
		return failServe(stderr, fmt.Errorf("proxy runtime settings: %w", err))
	}
	// Reject SNI values no website binds before any certificate lookup.
	certificatesManager.SetHostFilter(business.HasHost)
	business.TraceID = func(ctx context.Context) string {
		span := trace.SpanContextFromContext(ctx)
		if !span.IsValid() {
			return ""
		}
		return span.TraceID().String()
	}
	business.InternalPanic = func(err error) {
		events.Record(context.Background(), "runtime", "internal_panic", "critical", "RUNTIME_INTERNAL_PANIC", err)
	}
	health.setSnapshot(snapshot.Revision, time.Now().UTC())
	metrics.SetConfigRevision(snapshot.Revision, snapshot.Revision)
	store.SetSystemConfigRevision(snapshot.Revision)
	var appliedRevision atomic.Int64
	appliedRevision.Store(snapshot.Revision)
	var restartGeneration atomic.Int64
	var observedRestartGeneration atomic.Int64
	restartGeneration.Store(registered.RestartGeneration)
	observedRestartGeneration.Store(registered.ObservedRestartGeneration)
	var restartRequested atomic.Bool
	// Persist the first good database snapshot too; waiting for a later config
	// notification would leave a freshly installed node without offline LKG.
	if err := persistRuntimeLKG(cfg.String("node", "snapshotFile"), snapshot, certificateEntries); err != nil {
		logger.Error("persist initial LKG snapshot", "error", err)
	}
	if err := persistCoreRuntime(snapshot, certificateEntries, coreListeners, proxySettings); err != nil {
		logger.Error("retain prior core runtime after initial snapshot failure", "error", err)
	}
	var telemetryQueue *telemetry.AggregationQueue
	var telemetryAggregator *telemetry.Aggregator
	dirtyPeriods := make(chan telemetry.PeriodHint, 4096)
	var materializer *analytics.Materializer
	if cfg.Bool("resourceReports", "enabled") {
		materializer = &analytics.Materializer{
			Store:     store,
			Logger:    logger,
			Metrics:   metrics,
			Interval:  telemetryFlushInterval,
			Schedule:  analytics.Schedule{MinutelyDelay: resourceMinutelyDelay, HourlyDelay: resourceHourlyDelay, DailyDelay: resourceDailyDelay},
			UAParsing: cfg.Bool("trafficAggregation", "enableUAParsing"),
			Settings: analytics.Settings{
				MinutelyLookback:       resourceMinutelyReconciliationLookback,
				ReconciliationLookback: resourceReconciliationLookback,
				StatementTimeout:       resourceMaterializationStatementTimeout,
			},
			DirtyPeriods: dirtyPeriods,
		}
		materializer.Catchup(lifecycle)
	}
	trafficPolicy := telemetry.TrafficPolicy{Enabled: cfg.Bool("trafficAggregation", "enabled"), ExcludedPathPrefixes: cfg.Strings("trafficAggregation", "excludedPathPrefixes")}
	if cfg.Bool("trafficAggregation", "enabled") {
		telemetryQueue = telemetry.NewAggregationQueue(cfg.Int("trafficAggregation", "queueCapacity"), int64(cfg.Int("trafficAggregation", "maxQueueBytes")))
		telemetrySink := telemetry.NewAggregatorWithSettings(store, identityValue.NodeID, cfg.Bool("trafficAggregation", "enableUAParsing"), cfg.Int("trafficAggregation", "uaCacheSize"), cfg.Bool("trafficAggregation", "updateCurrentPeriod"))
		telemetryAggregator = telemetrySink
		telemetrySink.SetPeriodHintSink(func(hints []telemetry.PeriodHint) {
			for _, hint := range hints {
				select {
				case dirtyPeriods <- hint:
				default:
				}
			}
		})
		go telemetryQueue.Run(lifecycle, telemetryFlushInterval, cfg.Int("trafficAggregation", "batchSize"), telemetrySink)
		if materializer != nil {
			go materializer.Run(lifecycle)
		}
	}
	go retention.Telemetry{
		Store:                 store,
		Logger:                logger,
		Events:                quietEvents,
		ContributionRetention: contributionRetention,
		MonitorInfoRetention:  monitorInfoRetention,
		TrafficDays:           cfg.Int("trafficAggregation", "retentionDays"),
		ReportDays:            analytics.EffectiveDailyRetention(cfg.Int("resourceReports", "dailyRetentionDays"), cfg.Int("resourceReports", "reportRetentionDays")),
		MinuteReportDays:      cfg.Int("resourceReports", "minutelyRetentionDays"),
		HourlyReportDays:      cfg.Int("resourceReports", "hourlyRetentionDays"),
		HistoryHours:          cfg.Int("requestHistory", "retentionHours"),
		BatchSize:             cfg.Int("requestHistory", "retentionDeleteBatchSize"),
	}.Run(lifecycle)
	go retention.ConfigEvents{Store: store, Logger: logger, Retention: configEventRetention, InactiveAfter: inactiveNodeAfter}.Run(lifecycle)
	go retention.AccountSessions{Store: store, Logger: logger}.Run(lifecycle)
	historyQueue := telemetry.NewQueueWithLimits(cfg.Int("requestHistory", "queueCapacity"), int64(cfg.Int("requestHistory", "maxQueueBytes")))
	blacklistQueue := telemetry.NewQueue(10000)
	go blacklistQueue.RunWithRetries(lifecycle, time.Second, 1000, telemetry.BlacklistHitSink{Store: store}, 3)
	historyPolicy := telemetry.HistoryPolicy{
		Enabled:              cfg.Bool("requestHistory", "enabled"),
		AllMetadata:          cfg.String("requestHistory", "recordMode") == "AllMetadata",
		SuccessSamplingRate:  cfg.Float("requestHistory", "successSamplingRate"),
		ExcludedPathPrefixes: cfg.Strings("requestHistory", "excludedPathPrefixes"),
	}
	business.HistoryCapture = &proxy.HistoryCapture{
		BodyMaxBytes:     cfg.Int("requestHistory", "bodyCaptureMaxBytes"),
		HeaderMaxBytes:   cfg.Int("requestHistory", "maxHeaderBytes"),
		BodyContentTypes: cfg.Strings("requestHistory", "bodyCaptureContentTypes"),
		DecideEvent:      historyPolicy.Decide,
	}
	business.RecordHistory = historyQueue.Record
	business.RecordAggregation = func(event telemetry.AggregationEvent) {
		if telemetryQueue != nil && trafficPolicy.Enabled {
			telemetryQueue.Record(event)
		}
	}
	business.ShouldAggregate = trafficPolicy.ShouldAggregate
	business.RecordUnknownHosts = cfg.Bool("trafficAggregation", "recordUnknownHosts")
	business.Record = func(event telemetry.Event) {
		metrics.ObserveProxy(event)
		if event.BlacklistRuleID != 0 {
			blacklistQueue.Record(event)
		}
	}
	go historyQueue.RunWithRetries(lifecycle, historyFlushInterval, cfg.Int("requestHistory", "batchSize"), telemetry.HistorySink{Store: store}, cfg.Int("requestHistory", "maxFlushRetryCount"))
	go runMetricsMaintenance(lifecycle, pool, telemetryQueue, historyQueue, telemetryAggregator, metrics, events)
	{
		acmeScanInterval, e := cfg.Duration("acme", "scanIntervalMinimum")
		if e != nil {
			return failServe(stderr, e)
		}
		acmeRenewBefore, e := cfg.Duration("acme", "renewBefore")
		if e != nil {
			return failServe(stderr, e)
		}
		acmeLockRetry, e := cfg.Duration("acme", "lockRetryBase")
		if e != nil {
			return failServe(stderr, e)
		}
		acmeLockJitter, e := cfg.Duration("acme", "lockRetryJitterMaximum")
		if e != nil {
			return failServe(stderr, e)
		}
		acmeTemporaryMaximum, e := cfg.Duration("acme", "temporaryRetryMaximum")
		if e != nil {
			return failServe(stderr, e)
		}
		acmeKeyValueTimeout, e := cfg.Duration("acme", "keyValueRequestTimeout")
		if e != nil {
			return failServe(stderr, e)
		}
		keyValueBaseURL, e := cfg.ACMEKeyValueBaseURL()
		if e != nil {
			return failServe(stderr, e)
		}
		if configured := cfg.ACMEKeyValueEndpointURL(); configured != "" {
			probeClient := &http.Client{Timeout: acmeKeyValueTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
			if e := acme.ValidateRemoteEndpoint(ctx, configured, clusterID, identityValue.NodeID, probeClient); e != nil {
				return failServe(stderr, e)
			}
		}
		logger.Info("ACME key-value provider configured", "providerURL", keyValueBaseURL, "localServerEnabled", true)
		provider := acme.KeyValueProvider{BaseURL: keyValueBaseURL, SetPath: cfg.String("acme", "keyValueSetPath"), GetPath: cfg.String("acme", "keyValueGetPath"), Client: &http.Client{Timeout: acmeKeyValueTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
		business.ACMEChallenge = provider.Resolve
		acmeWorker, e = acme.NewWorker(store, pool, identityValue.NodeID, acme.Settings{DirectoryURL: cfg.String("acme", "directoryURL"), Email: cfg.String("acme", "email"), ScanInterval: acmeScanInterval, RenewBefore: acmeRenewBefore, LockRetryBase: acmeLockRetry, LockRetryJitter: acmeLockJitter, TemporaryRetryMax: acmeTemporaryMaximum, Provider: provider, ObserveAttempt: metrics.ObserveACMEAttempt, ObserveLockConflict: metrics.ObserveACMELockConflict, RecordSystemError: func(operation, code string, err error) {
			events.Record(lifecycle, "certificate", operation, "warning", code, err)
		}}, logger)
		if e != nil {
			_ = adminServer.Shutdown(context.Background())
			return failServe(stderr, e)
		}
	}
	var reloadMu sync.Mutex
	reload := func(_ int64) {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		started := time.Now()
		candidate, incremental, reloadCertificates, err := store.ApplyConfigEvents(lifecycle, business.Snapshot(), incrementalBatchSize)
		mode := "incremental"
		entries := certificatesManager.Entries()
		if err != nil {
			logger.Error("retain LKG after incremental proxy reload failure", "error", err)
			events.Record(lifecycle, "configsync", "incremental_reload", "error", "CONFIG_INCREMENTAL_RELOAD_FAILED", err)
			metrics.ObserveConfigRefresh(mode, "failure", time.Since(started).Seconds())
			return
		}
		if !incremental {
			mode = "full"
			candidate, err = store.LoadProxySnapshot(lifecycle)
			if err != nil {
				logger.Error("retain LKG after proxy reload failure", "error", err)
				events.Record(lifecycle, "configsync", "full_reload", "error", "CONFIG_SNAPSHOT_RELOAD_FAILED", err)
				metrics.ObserveConfigRefresh(mode, "failure", time.Since(started).Seconds())
				return
			}
			entries, err = store.LoadCertificates(lifecycle)
			if err != nil {
				logger.Error("retain LKG after certificate reload failure", "error", err)
				events.Record(lifecycle, "certificate", "reload", "error", "CERTIFICATE_RELOAD_FAILED", err)
				metrics.ObserveConfigRefresh(mode, "failure", time.Since(started).Seconds())
				return
			}
			if err := certificatesManager.Replace(entries); err != nil {
				logger.Error("retain LKG after certificate validation failure", "error", err)
				events.Record(lifecycle, "certificate", "reload_validate", "error", "CERTIFICATE_RELOAD_VALIDATION_FAILED", err)
				metrics.ObserveConfigRefresh(mode, "failure", time.Since(started).Seconds())
				return
			}
			setCertificateMetrics(metrics, "database", entries)
		} else if reloadCertificates {
			entries, err = store.LoadCertificates(lifecycle)
			if err != nil {
				logger.Error("retain LKG after incremental certificate reload failure", "error", err)
				events.Record(lifecycle, "certificate", "incremental_reload", "error", "CERTIFICATE_INCREMENTAL_RELOAD_FAILED", err)
				metrics.ObserveConfigRefresh(mode, "failure", time.Since(started).Seconds())
				return
			}
			if err := certificatesManager.Replace(entries); err != nil {
				logger.Error("retain LKG after incremental certificate validation failure", "error", err)
				events.Record(lifecycle, "certificate", "incremental_validate", "error", "CERTIFICATE_INCREMENTAL_VALIDATION_FAILED", err)
				metrics.ObserveConfigRefresh(mode, "failure", time.Since(started).Seconds())
				return
			}
			setCertificateMetrics(metrics, "database", entries)
		}
		if material, materialErr := store.LoadRuntimeMaterial(lifecycle); materialErr != nil {
			logger.Error("retain JWT material after reload failure", "error", materialErr)
			events.Record(lifecycle, "database", "runtime_material_reload", "error", "RUNTIME_MATERIAL_RELOAD_FAILED", materialErr)
		} else {
			activeKeys.Store(auth.Keys{Current: material.JWTCurrent, Previous: material.JWTPrevious, Issuer: cfg.String("security", "jwtIssuer"), Audience: cfg.String("security", "jwtAudience"), Session: time.Duration(cfg.Int("security", "sessionSeconds")) * time.Second})
		}
		if candidate == business.Snapshot() {
			return
		}
		business.Replace(candidate)
		appliedRevision.Store(candidate.Revision)
		health.setSnapshot(candidate.Revision, time.Now().UTC())
		metrics.SetConfigRevision(candidate.Revision, candidate.Revision)
		store.SetSystemConfigRevision(candidate.Revision)
		metrics.ObserveConfigRefresh(mode, "success", time.Since(started).Seconds())
		if err := persistRuntimeLKG(cfg.String("node", "snapshotFile"), candidate, entries); err != nil {
			logger.Error("persist LKG snapshot", "error", err)
		}
		if err := persistCoreRuntime(candidate, entries, coreListeners, proxySettings); err != nil {
			logger.Error("retain prior core runtime after snapshot reload failure", "error", err)
		}
	}
	go configsync.ListenWithOptions(lifecycle, pool, cfg.String("configSync", "channel"), configsync.ListenOptions{NotifyDebounce: notifyDebounce, ReconnectInitialBackoff: reconnectInitialBackoff, ReconnectMaximumBackoff: reconnectMaximumBackoff}, func() { reload(0) }, func(result string) {
		metrics.ObserveConfigNotifyReconnect(result)
		if result == "failure" {
			events.Record(lifecycle, "configsync", "listen", "warning", "CONFIGSYNC_LISTENER_FAILED", errors.New("configuration notification listener disconnected"))
		}
	})
	go configsync.Poll(lifecycle, revisionPollInterval, store, reload, func(err error) {
		events.Record(lifecycle, "configsync", "poll", "warning", "CONFIGSYNC_REVISION_POLL_FAILED", err)
	})
	running, err := listeners.Start(listeners.Config{HTTP: httpAddress, ProxyHTTP: proxyHTTPAddress, HTTPS: httpsAddress, ProxyHTTPS: proxyHTTPSAddress, HTTP3: h3Address, TrustedCIDRs: cfg.Strings("listeners.proxyProtocol", "trustedCIDRs"), RequireProxyProtocol: cfg.Bool("listeners.proxyProtocol", "requiredOnProxyListeners"), HeaderTimeout: proxyHeaderTimeout, ActivityTimeout: proxySettings.ActivityTimeout, HTTP3MaxIdleTimeout: h3IdleTimeout, MaxConcurrentRequests: proxySettings.MaxConcurrentRequests, MaxRequestHeaderBytes: proxySettings.MaxRequestHeaderBytes, MaxRequestBodyBytes: proxySettings.MaxRequestBodyBytes, TLS: certificatesManager.TLSConfig(), Handler: business, Logger: logger, TrackRequest: metrics.TrackProxyRequest, ObserveConcurrentRejection: metrics.ObserveConcurrentRejection, ObserveTLSHandshakeRejection: metrics.ObserveTLSHandshakeRejection, Instrument: tracing.Handler})
	if err != nil {
		_ = adminServer.Shutdown(context.Background())
		events.Record(ctx, "listener", "start", "critical", "LISTENER_START_FAILED", err)
		fmt.Fprintf(stderr, "start business listeners: %v\n", err)
		return 1
	}
	health.setReady([]string{"8080/tcp", "9080/tcp", "8443/tcp", "8443/udp", "9443/tcp"})
	if err := store.MarkNodeReady(lifecycle, identityValue.NodeID); err != nil {
		logger.Warn("mark node ready after restart", "node", identityValue.NodeID, "error", err)
	}
	go nodes.Monitor{
		Store:    store,
		Logger:   logger,
		Metrics:  metrics,
		Events:   events,
		NodeID:   identityValue.NodeID,
		Interval: heartbeatInterval,
		Timeout:  drainTimeout,
		Revisions: nodes.Revisions{
			Applied:           &appliedRevision,
			RestartGeneration: &restartGeneration,
			ObservedRestart:   &observedRestartGeneration,
			RestartRequested:  &restartRequested,
		},
		Drain: running.Close,
		Stop:  cancel,
	}.Run(lifecycle)
	if acmeWorker != nil {
		go acmeWorker.Run(lifecycle)
	}
	logger.Info("gateway listeners started", "node", identityValue.NodeID, "management", adminAddress)
	<-lifecycle.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer shutdownCancel()
	_ = adminServer.Shutdown(shutdownCtx)
	_ = running.Close(shutdownCtx)
	if restartRequested.Load() {
		// yaginx.service uses Restart=on-failure, so this dedicated exit code
		// turns a database-issued rolling restart request into a service restart.
		return 75
	}
	return 0
}

func registerLoopbackProfiles(admin *http.ServeMux) {
	loopback := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			address, err := netip.ParseAddr(host)
			if err != nil || !address.IsLoopback() {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	admin.Handle("/debug/pprof/", loopback(http.HandlerFunc(pprof.Index)))
	admin.Handle("/debug/pprof/cmdline", loopback(http.HandlerFunc(pprof.Cmdline)))
	admin.Handle("/debug/pprof/profile", loopback(http.HandlerFunc(pprof.Profile)))
	admin.Handle("/debug/pprof/symbol", loopback(http.HandlerFunc(pprof.Symbol)))
	admin.Handle("/debug/pprof/trace", loopback(http.HandlerFunc(pprof.Trace)))
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		admin.Handle("/debug/pprof/"+name, loopback(pprof.Handler(name)))
	}
}

func failServe(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "runtime listener configuration: %v\n", err)
	return 1
}
