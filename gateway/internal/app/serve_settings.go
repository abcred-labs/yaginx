package app

// The runtime document is the single source of serve's tunables. Parsing it
// into typed settings here keeps runServe free of validation.

import (
	"fmt"
	"time"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/proxy"
)

type serveSettings struct {
	ProxyHeaderTimeout                      time.Duration
	TelemetryFlushInterval                  time.Duration
	HistoryFlushInterval                    time.Duration
	MonitorInfoRetention                    time.Duration
	ContributionRetention                   time.Duration
	ResourceMinutelyDelay                   time.Duration
	ResourceHourlyDelay                     time.Duration
	ResourceDailyDelay                      time.Duration
	ResourceReconciliationLookback          time.Duration
	ResourceMinutelyReconciliationLookback  time.Duration
	ResourceMaterializationStatementTimeout time.Duration
	ConfigEventRetention                    time.Duration
	InactiveNodeAfter                       time.Duration
	IncrementalBatchSize                    int
	RevisionPollInterval                    time.Duration
	NotifyDebounce                          time.Duration
	ReconnectInitialBackoff                 time.Duration
	ReconnectMaximumBackoff                 time.Duration
	ShutdownGracePeriod                     time.Duration
	HeartbeatInterval                       time.Duration
	HeartbeatStaleAfter                     time.Duration
	DrainTimeout                            time.Duration
}

func parseServeSettings(cfg config.RuntimeDocument) (serveSettings, error) {
	duration := func(section, key string) (time.Duration, error) {
		return cfg.Duration(section, key)
	}
	var out serveSettings
	var err error
	if out.ProxyHeaderTimeout, err = duration("listeners.proxyProtocol", "headerTimeout"); err != nil {
		return out, err
	}
	if out.TelemetryFlushInterval, err = duration("trafficAggregation", "flushInterval"); err != nil {
		return out, err
	}
	if out.HistoryFlushInterval, err = duration("requestHistory", "flushInterval"); err != nil {
		return out, err
	}
	if out.MonitorInfoRetention, err = duration("resourceReports", "monitorInfoRetention"); err != nil {
		return out, err
	}
	if out.ContributionRetention, err = duration("resourceReports", "contributionRetention"); err != nil {
		return out, err
	}
	if out.ContributionRetention < 72*time.Hour || out.ContributionRetention > 8760*time.Hour {
		return out, fmt.Errorf("resourceReports.contributionRetention must be between 72h and 8760h")
	}
	if out.ResourceMinutelyDelay, err = duration("resourceReports", "minutelyDelay"); err != nil {
		return out, err
	}
	if out.ResourceHourlyDelay, err = duration("resourceReports", "hourlyDelay"); err != nil {
		return out, err
	}
	if out.ResourceDailyDelay, err = duration("resourceReports", "dailyDelay"); err != nil {
		return out, err
	}
	if out.ResourceReconciliationLookback, err = duration("resourceReports", "reconciliationLookback"); err != nil {
		return out, err
	}
	if out.ResourceMinutelyReconciliationLookback, err = duration("resourceReports", "minutelyReconciliationLookback"); err != nil {
		return out, err
	}
	if out.ResourceMinutelyReconciliationLookback <= 0 {
		return out, fmt.Errorf("resourceReports.minutelyReconciliationLookback must be positive")
	}
	if out.ResourceMaterializationStatementTimeout, err = duration("resourceReports", "materializationStatementTimeout"); err != nil {
		return out, err
	}
	if out.ResourceMaterializationStatementTimeout <= 0 {
		return out, fmt.Errorf("resourceReports.materializationStatementTimeout must be positive")
	}
	if out.ConfigEventRetention, err = duration("configSync", "eventRetention"); err != nil {
		return out, err
	}
	if out.InactiveNodeAfter, err = duration("configSync", "inactiveNodeAfter"); err != nil {
		return out, err
	}
	out.IncrementalBatchSize = cfg.Int("configSync", "incrementalBatchSize")
	if out.RevisionPollInterval, err = duration("configSync", "revisionPollInterval"); err != nil {
		return out, err
	}
	if out.NotifyDebounce, err = duration("configSync", "notifyDebounce"); err != nil {
		return out, err
	}
	if out.ReconnectInitialBackoff, err = duration("configSync", "reconnectInitialBackoff"); err != nil {
		return out, err
	}
	if out.ReconnectMaximumBackoff, err = duration("configSync", "reconnectMaximumBackoff"); err != nil {
		return out, err
	}
	if out.ShutdownGracePeriod, err = duration("node", "shutdownGracePeriod"); err != nil {
		return out, err
	}
	if out.HeartbeatInterval, err = duration("node", "heartbeatInterval"); err != nil {
		return out, err
	}
	if out.HeartbeatStaleAfter, err = duration("node", "heartbeatStaleAfter"); err != nil {
		return out, err
	}
	if out.DrainTimeout, err = duration("node", "drainTimeout"); err != nil {
		return out, err
	}
	return out, nil
}

func runtimePoolOptions(cfg config.RuntimeDocument) (database.PoolOptions, error) {
	connectTimeout, err := cfg.Duration("database", "connectTimeout")
	if err != nil {
		return database.PoolOptions{}, err
	}
	statementTimeout, err := cfg.Duration("database", "statementTimeout")
	if err != nil {
		return database.PoolOptions{}, err
	}
	healthTimeout, err := cfg.Duration("database", "healthQueryTimeout")
	if err != nil {
		return database.PoolOptions{}, err
	}
	maxLifetime, err := cfg.Duration("database", "maxConnectionLifetime")
	if err != nil {
		return database.PoolOptions{}, err
	}
	maxIdle, err := cfg.Duration("database", "maxConnectionIdleTime")
	if err != nil {
		return database.PoolOptions{}, err
	}
	return database.PoolOptions{MinimumServerMajor: cfg.Int("database", "minimumServerMajor"), MaxConnections: cfg.Int("database", "maxConnections"), MinConnections: cfg.Int("database", "minConnections"), ConnectTimeout: connectTimeout, StatementTimeout: statementTimeout, HealthQueryTimeout: healthTimeout, MaxConnectionLifetime: maxLifetime, MaxConnectionIdleTime: maxIdle}, nil
}

func runtimeProxySettings(cfg config.RuntimeDocument) (proxy.Settings, error) {
	activityTimeout, err := cfg.Duration("proxy", "activityTimeout")
	if err != nil {
		return proxy.Settings{}, err
	}
	responseHeaderTimeout, err := cfg.Duration("proxy", "responseHeaderTimeout")
	if err != nil {
		return proxy.Settings{}, err
	}
	idleConnectionTimeout, err := cfg.Duration("proxy", "idleConnectionTimeout")
	if err != nil {
		return proxy.Settings{}, err
	}
	expectContinueTimeout, err := cfg.Duration("proxy", "expectContinueTimeout")
	if err != nil {
		return proxy.Settings{}, err
	}
	return proxy.Settings{
		ActivityTimeout:           activityTimeout,
		ResponseHeaderTimeout:     responseHeaderTimeout,
		MaxConcurrentRequests:     cfg.Int("proxy", "maxConcurrentRequests"),
		MaxRequestHeaderBytes:     cfg.Int("proxy", "maxRequestHeaderBytes"),
		MaxRequestBodyBytes:       cfg.Int64("proxy", "maxRequestBodyBytes"),
		MaxIdleConnections:        cfg.Int("proxy", "maxIdleConnections"),
		MaxIdleConnectionsPerHost: cfg.Int("proxy", "maxIdleConnectionsPerHost"),
		IdleConnectionTimeout:     idleConnectionTimeout,
		ExpectContinueTimeout:     expectContinueTimeout,
		UpstreamExpectContinue: proxy.ExpectContinuePolicy{
			Enabled:               cfg.Bool("proxy.upstreamExpectContinue", "enabled"),
			MinContentLengthBytes: int64(cfg.Int("proxy.upstreamExpectContinue", "minContentLengthBytes")),
			IncludedHosts:         cfg.Strings("proxy.upstreamExpectContinue", "includedHosts"),
			IncludedPathPrefixes:  cfg.Strings("proxy.upstreamExpectContinue", "includedPathPrefixes"),
		},
		IPBlacklistCIDRs: cfg.Strings("proxy", "ipBlacklistCIDRs"),
	}, nil
}
