package management

type APICredential struct {
	ID          ID     `json:"id"`
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	TokenPrefix string `json:"tokenPrefix"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	RevokedAt   *int64 `json:"revokedAt"`
}

type APICredentialSecret struct {
	APICredential
	Token string `json:"token"`
}

type APICredentialCreateRequest struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

// MetricItem is a named counter in an observability breakdown.
type MetricItem struct {
	Type  string `json:"type"`
	Value int64  `json:"value"`
}

type AnalyticsKpis struct {
	Requests               int64    `json:"requests"`
	RequestsPerSecond      float64  `json:"requestsPerSecond"`
	InboundBytes           int64    `json:"inboundBytes"`
	OutboundBytes          int64    `json:"outboundBytes"`
	InboundBytesPerSecond  float64  `json:"inboundBytesPerSecond"`
	OutboundBytesPerSecond float64  `json:"outboundBytesPerSecond"`
	ErrorRequests          int64    `json:"errorRequests"`
	ErrorRate              float64  `json:"errorRate"`
	P50Ms                  *float64 `json:"p50Ms"`
	P95Ms                  *float64 `json:"p95Ms"`
	P99Ms                  *float64 `json:"p99Ms"`
	AvgDurationMs          float64  `json:"avgDurationMs"`
	MaxDurationMs          float64  `json:"maxDurationMs"`
	LatencyCoverageRate    float64  `json:"latencyCoverageRate"`
}

type AnalyticsLatency struct {
	P50Ms *float64 `json:"p50Ms"`
	P95Ms *float64 `json:"p95Ms"`
	P99Ms *float64 `json:"p99Ms"`
	AvgMs float64  `json:"avgMs"`
}

type AnalyticsModeMetrics struct {
	Requests               int64            `json:"requests"`
	CompletionLatency      AnalyticsLatency `json:"completionLatency"`
	FirstByteLatency       AnalyticsLatency `json:"firstByteLatency"`
	FirstTokenLatency      AnalyticsLatency `json:"firstTokenLatency"`
	CompletionCoverageRate float64          `json:"completionCoverageRate"`
	FirstByteCoverageRate  float64          `json:"firstByteCoverageRate"`
	FirstTokenCoverageRate float64          `json:"firstTokenCoverageRate"`
}

type AnalyticsModeBreakdown struct {
	NonStream AnalyticsModeMetrics `json:"nonStream"`
	Stream    AnalyticsModeMetrics `json:"stream"`
	Upgrade   AnalyticsModeMetrics `json:"upgrade"`
}

type AnalyticsUserAgentCoverage struct {
	BrowserRate float64 `json:"browserRate"`
	OSRate      float64 `json:"osRate"`
	SpiderRate  float64 `json:"spiderRate"`
}

type AnalyticsUserAgents struct {
	Browsers         []MetricItem               `json:"browsers"`
	Spiders          []MetricItem               `json:"spiders"`
	OperatingSystems []MetricItem               `json:"operatingSystems"`
	Coverage         AnalyticsUserAgentCoverage `json:"coverage"`
}

type AnalyticsCoverage struct {
	From           int64  `json:"from"`
	To             int64  `json:"to"`
	IsComplete     bool   `json:"isComplete"`
	State          string `json:"state"`
	MissingBuckets int    `json:"missingBuckets"`
	WindowBuckets  int    `json:"windowBuckets"`
}

type AnalyticsBase struct {
	AsOf     int64             `json:"asOf"`
	From     int64             `json:"from"`
	To       int64             `json:"to"`
	Grain    string            `json:"grain"`
	Source   string            `json:"source"`
	Coverage AnalyticsCoverage `json:"coverage"`
}

type AnalyticsDomain struct {
	AnalyticsKpis
	Domain          string                 `json:"domain"`
	IsPartial       bool                   `json:"isPartial"`
	StatusBreakdown []MetricItem           `json:"statusBreakdown"`
	DurationBuckets []MetricItem           `json:"durationBuckets"`
	UserAgents      AnalyticsUserAgents    `json:"userAgents"`
	Modes           AnalyticsModeBreakdown `json:"modes"`
}

type AnalyticsPoint struct {
	AnalyticsKpis
	BucketStartAt   int64                  `json:"bucketStartAt"`
	IsPartial       bool                   `json:"isPartial"`
	StatusBreakdown []MetricItem           `json:"statusBreakdown"`
	DurationBuckets []MetricItem           `json:"durationBuckets"`
	Modes           AnalyticsModeBreakdown `json:"modes"`
}

type AnalyticsSummary struct {
	AnalyticsBase
	CurrentInFlight int64                  `json:"currentInFlight"`
	DataLagSeconds  int64                  `json:"dataLagSeconds"`
	HasData         bool                   `json:"hasData"`
	IsPartial       bool                   `json:"isPartial"`
	ActiveHostCount int                    `json:"activeHostCount"`
	Kpis            AnalyticsKpis          `json:"kpis"`
	StatusBreakdown []MetricItem           `json:"statusBreakdown"`
	DurationBuckets []MetricItem           `json:"durationBuckets"`
	ErrorBreakdown  []MetricItem           `json:"errorBreakdown"`
	UserAgents      AnalyticsUserAgents    `json:"userAgents"`
	TopDomains      []AnalyticsDomain      `json:"topDomains"`
	Modes           AnalyticsModeBreakdown `json:"modes"`
}

type AnalyticsDomains struct {
	AnalyticsBase
	Domains []AnalyticsDomain `json:"domains"`
}

type AnalyticsTimeseries struct {
	AnalyticsBase
	Domain string           `json:"domain"`
	Points []AnalyticsPoint `json:"points"`
}

type AnalyticsHostRanking struct {
	HostName      string  `json:"hostName"`
	RequestCounts int64   `json:"requestCounts"`
	InboundBytes  int64   `json:"inboundBytes"`
	OutboundBytes int64   `json:"outboundBytes"`
	TotalBytes    int64   `json:"totalBytes"`
	Share         float64 `json:"share"`
}

type AnalyticsContributionPoint struct {
	Period        int64  `json:"period"`
	HostName      string `json:"hostName"`
	RequestCounts int64  `json:"requestCounts"`
	IsPartial     bool   `json:"isPartial"`
}

type AnalyticsContributionSummary struct {
	TotalRequests       int64  `json:"totalRequests"`
	ActiveHostCount     int    `json:"activeHostCount"`
	TopHostName         string `json:"topHostName"`
	TopHostRequestCount int64  `json:"topHostRequestCounts"`
}

type AnalyticsContributions struct {
	AnalyticsBase
	Summary  AnalyticsContributionSummary `json:"summary"`
	Rankings []AnalyticsHostRanking       `json:"rankings"`
	Timeline []AnalyticsContributionPoint `json:"timeline"`
}

type AnalyticsDetail struct {
	ID            string   `json:"id"`
	HostName      string   `json:"hostName"`
	Period        int64    `json:"period"`
	RequestCounts int64    `json:"requestCounts"`
	InboundBytes  int64    `json:"inboundBytes"`
	OutboundBytes int64    `json:"outboundBytes"`
	TotalBytes    int64    `json:"totalBytes"`
	ErrorRequests int64    `json:"errorRequests"`
	ErrorRate     float64  `json:"errorRate"`
	AvgDurationMs float64  `json:"avgDurationMs"`
	P95Ms         *float64 `json:"p95Ms"`
	IsPartial     bool     `json:"isPartial"`
}

type AnalyticsDetails struct {
	AnalyticsBase
	Records []AnalyticsDetail `json:"records"`
	Paging  PageCursor        `json:"paging"`
}

// AnalyticsQuery is the request contract shared by every analytics operation.
// A zero window falls back to the service default, and the grain is always
// derived from the window width rather than requested.
type AnalyticsQuery struct {
	From    int64    `json:"from"`
	To      int64    `json:"to"`
	Domain  string   `json:"domain"`
	Domains []string `json:"domains,omitempty"`
}

type AnalyticsPagedQuery struct {
	AnalyticsQuery
	PageRequest
}

type AnalyticsOverviewQuery struct {
	AnalyticsQuery
	Sections []string `json:"sections"`
}

type AnalyticsOverview struct {
	AnalyticsBase
	Overview *AnalyticsSummary `json:"overview,omitempty"`
	Domains  []AnalyticsDomain `json:"domains,omitempty"`
	Points   []AnalyticsPoint  `json:"points,omitempty"`
}

type DatabaseCapability struct {
	Enabled   bool   `json:"enabled"`
	Installed bool   `json:"installed"`
	Queryable bool   `json:"queryable"`
	Reason    string `json:"reason"`
}

type DatabaseHealth struct {
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

type DatabasePoolStats struct {
	Total        int64   `json:"total"`
	Acquired     int64   `json:"acquired"`
	Idle         int64   `json:"idle"`
	Constructing int64   `json:"constructing"`
	Max          int64   `json:"max"`
	UsageRate    float64 `json:"usageRate"`
}

type DatabaseStats struct {
	Name         string  `json:"name"`
	Connections  int64   `json:"connections"`
	Commits      int64   `json:"commits"`
	Rollbacks    int64   `json:"rollbacks"`
	BlocksRead   int64   `json:"blocksRead"`
	BlocksHit    int64   `json:"blocksHit"`
	CacheHitRate float64 `json:"cacheHitRate"`
	TempFiles    int64   `json:"tempFiles"`
	TempBytes    int64   `json:"tempBytes"`
	Deadlocks    int64   `json:"deadlocks"`
	BlockReadMs  float64 `json:"blockReadMs"`
	BlockWriteMs float64 `json:"blockWriteMs"`
	StatsResetAt int64   `json:"statsResetAt"`
}

type DatabaseActivity struct {
	Active             int64   `json:"active"`
	Idle               int64   `json:"idle"`
	IdleInTransaction  int64   `json:"idleInTransaction"`
	LockWaits          int64   `json:"lockWaits"`
	UngrantedLocks     int64   `json:"ungrantedLocks"`
	MaxLockWaitSeconds float64 `json:"maxLockWaitSeconds"`
}

type DatabaseIO struct {
	Available bool     `json:"available"`
	Reads     *int64   `json:"reads,omitempty"`
	Writes    *int64   `json:"writes,omitempty"`
	Extends   *int64   `json:"extends,omitempty"`
	Fsyncs    *int64   `json:"fsyncs,omitempty"`
	Evictions *int64   `json:"evictions,omitempty"`
	Reuses    *int64   `json:"reuses,omitempty"`
	ReadMs    *float64 `json:"readMs,omitempty"`
	WriteMs   *float64 `json:"writeMs,omitempty"`
}

type DatabaseTableStats struct {
	Name              string  `json:"name"`
	LiveTuples        int64   `json:"liveTuples"`
	DeadTuples        int64   `json:"deadTuples"`
	DeadTupleRatio    float64 `json:"deadTupleRatio"`
	SizeBytes         int64   `json:"sizeBytes"`
	LastAutovacuumAt  int64   `json:"lastAutovacuumAt"`
	LastAutoanalyzeAt int64   `json:"lastAutoanalyzeAt"`
}

type DatabaseTopSQL struct {
	QueryID           string  `json:"queryId"`
	Query             string  `json:"query"`
	Calls             int64   `json:"calls"`
	TotalPlanMs       float64 `json:"totalPlanMs"`
	TotalExecMs       float64 `json:"totalExecMs"`
	MeanPlanMs        float64 `json:"meanPlanMs"`
	MeanExecMs        float64 `json:"meanExecMs"`
	SharedBlocksRead  int64   `json:"sharedBlocksRead"`
	SharedBlocksHit   int64   `json:"sharedBlocksHit"`
	TempBlocksRead    int64   `json:"tempBlocksRead"`
	TempBlocksWritten int64   `json:"tempBlocksWritten"`
}

type DatabaseCapabilities struct {
	PGStatStatements DatabaseCapability `json:"pgStatStatements"`
	PGStatIO         DatabaseCapability `json:"pgStatIO"`
	TrackIOTiming    DatabaseCapability `json:"trackIOTiming"`
}

type DatabasePerformance struct {
	AsOf         int64                `json:"asOf"`
	Health       DatabaseHealth       `json:"health"`
	Pool         DatabasePoolStats    `json:"pool"`
	Database     DatabaseStats        `json:"database"`
	Activity     DatabaseActivity     `json:"activity"`
	IO           DatabaseIO           `json:"io"`
	Tables       []DatabaseTableStats `json:"tables"`
	Capabilities DatabaseCapabilities `json:"capabilities"`
	TopSQL       []DatabaseTopSQL     `json:"topSql"`
}

type GatewayRuntimeQueue struct {
	Depth      int64  `json:"depth"`
	BatchDepth int64  `json:"batchDepth"`
	Dropped    uint64 `json:"dropped"`
}

type GatewayRuntimeSnapshot struct {
	AsOf           int64                          `json:"asOf"`
	HeapAllocBytes uint64                         `json:"heapAllocBytes"`
	HeapInuseBytes uint64                         `json:"heapInuseBytes"`
	ResidentBytes  uint64                         `json:"residentBytes"`
	Goroutines     int                            `json:"goroutines"`
	ActiveRequests int64                          `json:"activeRequests"`
	Queues         map[string]GatewayRuntimeQueue `json:"queues"`
}

type GatewayHealthSummary struct {
	Level           string   `json:"level"`
	Reasons         []string `json:"reasons"`
	TotalNodes      int      `json:"totalNodes"`
	ActiveNodes     int      `json:"activeNodes"`
	StaleNodes      int      `json:"staleNodes"`
	DrainingNodes   int      `json:"drainingNodes"`
	LatestHeartbeat int64    `json:"latestHeartbeat"`
}

type DiagnosticsOverview struct {
	AsOf    int64                  `json:"asOf"`
	Health  GatewayHealthSummary   `json:"health"`
	Runtime GatewayRuntimeSnapshot `json:"runtime"`
}

type ResourceCertificate struct {
	Fingerprint string `json:"fingerprint"`
	NotBefore   int64  `json:"notBefore"`
	NotAfter    int64  `json:"notAfter"`
	IsActive    bool   `json:"isActive"`
}

type ResourceWebsiteReference struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
}

type ResourceHost struct {
	Host             string                     `json:"host"`
	ManagementStatus string                     `json:"managementStatus"`
	DomainConfig     *DomainListItem            `json:"domainConfig"`
	Websites         []ResourceWebsiteReference `json:"websites"`
	Certificate      *ResourceCertificate       `json:"certificate"`
	AutoRenew        *ResourceAutoRenew         `json:"autoRenew"`
	LastActivity     int64                      `json:"lastActivity"`
}

// ResourceAutoRenew is a host's automatic certificate request state. Status is
// "off" (auto-request disabled), "active", or "stopped" (stopped for good after
// consecutive failed days; a successful manual request resumes it).
type ResourceAutoRenew struct {
	Status        string                `json:"status"`
	StopReason    string                `json:"stopReason,omitempty"`
	FailDays      int                   `json:"failDays"`
	AttemptsToday int                   `json:"attemptsToday"`
	MaxPerDay     int                   `json:"maxPerDay"`
	LatestOrder   *ResourceACMEOrderRef `json:"latestOrder"`
}

type ResourceACMEOrderRef struct {
	ID        ID     `json:"id"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updatedAt"`
}

type ResourceCatalogQuery struct {
	Host               string
	Status             string
	WebsiteAssociation string
	PageRequest
}

type ResourceViewQuery struct {
	From int64
	To   int64
}

type DomainResourceViewQuery struct {
	ResourceViewQuery
	Host string
}

type WebsiteResourceViewQuery struct {
	ResourceViewQuery
	ID string
}

type ResourceCatalog struct {
	Records        []ResourceHost `json:"records"`
	Paging         PageCursor     `json:"paging"`
	ManagedTotal   int            `json:"managedTotal"`
	UnmanagedTotal int            `json:"unmanagedTotal"`
}

type AnalyticsSubjectOverview struct {
	AsOf            int64               `json:"asOf"`
	From            int64               `json:"from"`
	To              int64               `json:"to"`
	Grain           string              `json:"grain"`
	DataLagSeconds  int64               `json:"dataLagSeconds"`
	HasData         bool                `json:"hasData"`
	IsPartial       bool                `json:"isPartial"`
	Kpis            AnalyticsKpis       `json:"kpis"`
	ErrorBreakdown  []MetricItem        `json:"errorBreakdown"`
	StatusBreakdown []MetricItem        `json:"statusBreakdown"`
	DurationBuckets []MetricItem        `json:"durationBuckets"`
	UserAgents      AnalyticsUserAgents `json:"userAgents"`
	Coverage        AnalyticsCoverage   `json:"coverage"`
	Source          string              `json:"source"`
}

type AnalyticsSubjectTimeseries struct {
	AsOf    int64            `json:"asOf"`
	From    int64            `json:"from"`
	To      int64            `json:"to"`
	Grain   string           `json:"grain"`
	Domains []string         `json:"domains"`
	Points  []AnalyticsPoint `json:"points"`
}

type DomainResourceView struct {
	Host       ResourceHost               `json:"host"`
	Overview   AnalyticsSubjectOverview   `json:"overview"`
	Timeseries AnalyticsSubjectTimeseries `json:"timeseries"`
}

// AnalyticsSubjectView is one subject's configuration and analytics read model,
// unifying what the domain and website views used to return separately. Website
// is nil when SubjectType is "domain", and Hosts then carries the single
// matching Host.
type AnalyticsSubjectView struct {
	SubjectType string                     `json:"subjectType"`
	Website     *Website                   `json:"website"`
	Hosts       []ResourceHost             `json:"hosts"`
	Overview    AnalyticsSubjectOverview   `json:"overview"`
	Timeseries  AnalyticsSubjectTimeseries `json:"timeseries"`
}

type WebsiteResourceView struct {
	Website    Website                    `json:"website"`
	Hosts      []ResourceHost             `json:"hosts"`
	Overview   AnalyticsSubjectOverview   `json:"overview"`
	Timeseries AnalyticsSubjectTimeseries `json:"timeseries"`
}

// RequestHistorySearchQuery is the complete, typed filter accepted by the
// request-history search operation. Nil values intentionally mean no filter.
type RequestHistorySearchQuery struct {
	PageRequest
	WebsiteID          *ID         `json:"websiteId"`
	From               *Int64Input `json:"from"`
	To                 *Int64Input `json:"to"`
	Host               string      `json:"host"`
	Path               string      `json:"path"`
	StatusCode         *int        `json:"statusCode"`
	StatusFamily       string      `json:"statusFamily"`
	StatusCodePattern  string      `json:"statusCodePattern"`
	UpstreamStatusCode *int        `json:"upstreamStatusCode"`
	StatusSource       string      `json:"statusSource"`
	ErrorCategory      string      `json:"errorCategory"`
	ErrorCode          string      `json:"errorCode"`
	ClientIP           string      `json:"clientIp"`
	UserAgent          string      `json:"userAgent"`
}

// RequestHistoryListItem is the compact record returned by search. IDs and
// durations remain strings so JavaScript clients do not lose bigint precision.
type RequestHistoryListItem struct {
	ID                 string `json:"id"`
	RequestAt          int64  `json:"requestAt"`
	DurationMs         string `json:"durationMs"`
	ClientIP           string `json:"clientIp"`
	Method             string `json:"method"`
	Host               string `json:"host"`
	Path               string `json:"path"`
	StatusCode         int    `json:"statusCode"`
	UpstreamStatusCode *int   `json:"upstreamStatusCode"`
	StatusSource       string `json:"statusSource"`
	ErrorCategory      string `json:"errorCategory"`
	ErrorCode          string `json:"errorCode"`
	UserAgent          string `json:"userAgent"`
	ProxyStage         string `json:"proxyStage"`
	ExceptionStage     string `json:"exceptionStage"`
	ForwarderError     string `json:"forwarderError"`
	HasException       bool   `json:"hasException"`
	ExceptionType      string `json:"exceptionType"`
	TraceID            string `json:"traceId,omitempty"`
}

type RequestHistoryDetail struct {
	RequestHistoryListItem
	Scheme                string              `json:"scheme"`
	QueryString           string              `json:"queryString"`
	URL                   string              `json:"url"`
	Protocol              string              `json:"protocol"`
	RequestHeaders        map[string][]string `json:"requestHeaders"`
	ResponseHeaders       map[string][]string `json:"responseHeaders"`
	RequestBodyBase64     string              `json:"requestBodyBase64"`
	ResponseBodyBase64    string              `json:"responseBodyBase64"`
	RequestContentType    string              `json:"requestContentType"`
	ResponseContentType   string              `json:"responseContentType"`
	RequestBodyTruncated  bool                `json:"requestBodyTruncated"`
	ResponseBodyTruncated bool                `json:"responseBodyTruncated"`
	ExceptionMessage      string              `json:"exceptionMessage"`
	ExceptionStackTrace   string              `json:"exceptionStackTrace"`
}

type RequestHistoryPage = Page[RequestHistoryListItem]

type SystemErrorSearchQuery struct {
	PageRequest
	Component      string `json:"component"`
	Severity       string `json:"severity"`
	ErrorCode      string `json:"errorCode"`
	ConfigRevision *int64 `json:"configRevision,omitempty"`
	SystemEventID  string `json:"systemEventId,omitempty"`
}

type SystemErrorListItem struct {
	ID              string         `json:"id"`
	SystemEventID   string         `json:"systemEventId"`
	OccurredAt      int64          `json:"occurredAt"`
	NodeID          string         `json:"nodeId"`
	Version         string         `json:"version"`
	BuildID         string         `json:"buildId"`
	ConfigRevision  *int64         `json:"configRevision,omitempty"`
	Component       string         `json:"component"`
	Operation       string         `json:"operation"`
	Severity        string         `json:"severity"`
	ErrorCode       string         `json:"errorCode"`
	Message         string         `json:"message"`
	Details         map[string]any `json:"details,omitempty"`
	TraceID         string         `json:"traceId,omitempty"`
	OccurrenceCount int64          `json:"occurrenceCount"`
	FirstSeenAt     int64          `json:"firstSeenAt"`
	LastSeenAt      int64          `json:"lastSeenAt"`
}

type SystemErrorPage = Page[SystemErrorListItem]
