/** 冒烟用例使用的接口夹具：形状与管理端 API 一致，数值只求能让页面渲染。 */
const now = () => Math.floor(Date.now() / 1000);

export const envelope = (data: unknown, code = 200, msg: string | null = null) =>
  ({ code, msg, errMsg: msg, tid: 'e2e', data });

export const emptyPage = { records: [], paging: { pageIndex: 1, pageSize: 50, total: 0 } };

export const loginData = { token: 'e2e-token', expiresIn: 36000, name: 'admin@yaginx.local', userId: '1' };

export const runtimeConfig = () => ({
  proxy: { activityTimeout: '5m', responseHeaderTimeout: '30s', maxConcurrentRequests: 1024, maxRequestHeaderBytes: 65536, maxRequestBodyBytes: 0, maxIdleConnectionsPerHost: 64 },
  trafficAggregation: { enabled: true, flushInterval: '15s', retentionDays: 30 },
  requestHistory: { enabled: true, recordMode: 'ErrorsOnly', retentionHours: 72 },
  acme: { email: 'ops@example.com', keyValueEndpointURL: '', renewBefore: '168h' },
  observability: { logLevel: 'info', logFormat: 'json' },
  management: { allowedCIDRs: ['127.0.0.1/32'] },
  listeners: { proxyProtocol: { trustedCIDRs: ['10.0.0.0/8'] } },
});

export const website = (id = '5001', name = 'shop') => ({
  id,
  name,
  hasCertificate: true,
  defaultHost: `${name}.example.com`,
  webProxy: null,
  authorizationPolicy: null,
  preserveOriginalHost: true,
  allowInvalidTlsCertificate: false,
  redirectHttpToHttps: true,
  isMaintenanceMode: false,
  ipAccessMode: 'blacklist',
  ipAccessCIDRs: ['198.51.100.0/24'],
  hosts: [`${name}.example.com`, `www.${name}.example.com`],
  hostBindings: [
    { id: '6001', hostname: `${name}.example.com`, enabled: true, role: 'primary', accessMode: 'proxy', certificateMode: 'request', certificateId: null, verificationStatus: 'verified' },
    { id: '6002', hostname: `www.${name}.example.com`, enabled: true, role: 'alias', accessMode: 'redirect', redirectStatusCode: 301, redirectTargetHost: `${name}.example.com`, certificateMode: 'manual', certificateId: null, verificationStatus: 'pending' },
  ],
  upstreamPools: [{ id: 'pool-a', name: 'api-pool', strategy: 'weightedRoundRobin', endpoints: [{ address: 'http://10.0.0.5:8080', weight: 3, priority: 1, enabled: true }, { address: 'http://10.0.0.6:8080', weight: 1, priority: 2, enabled: true }] }],
  proxyRules: [
    { ruleId: '8001', name: 'api', isEnabled: true, pathPattern: '/api/', isRegexMatchPattern: false, targetType: 'pool', upstreamPoolId: 'pool-a', preserveOriginalHost: true },
    { ruleId: '8002', name: 'static', isEnabled: true, pathPattern: '/', destination: 'http://10.0.0.9:80', destinationHost: null, isFallback: true },
  ],
  proxyTransforms: [{ key: 'X-Forwarded-Prefix', value: '/app' }],
  simpleResponses: [{ path: '/healthz', contentType: 'text/plain', statusCode: 200, responseContent: 'ok', extraHeaders: [] }],
  requestHistory: { mode: 'inherit' },
});

const statuses = [{ type: '2xx', value: 11800 }, { type: '4xx', value: 300 }, { type: '5xx', value: 125 }];
const buckets = [{ type: '<50ms', value: 8000 }, { type: '50-200ms', value: 3500 }];
const latency = (a: number) => ({ p50Ms: a, p95Ms: a * 3, p99Ms: a * 5, avgMs: a });
const mode = (n: number) => ({ requests: n, completionLatency: latency(40), firstByteLatency: latency(20), firstTokenLatency: latency(0), completionCoverageRate: 0.98, firstByteCoverageRate: 0.97, firstTokenCoverageRate: 0 });
const modes = () => ({ nonStream: mode(9000), stream: mode(300), upgrade: mode(20) });
const kpis = (n = 12345) => ({ requests: n, requestsPerSecond: n / 3600, inboundBytes: n * 800, outboundBytes: n * 5400, inboundBytesPerSecond: 900, outboundBytesPerSecond: 6100, errorRequests: Math.round(n * 0.02), errorRate: 0.02, p50Ms: 35, p95Ms: 180, p99Ms: 420, avgDurationMs: 60, maxDurationMs: 2300, latencyCoverageRate: 0.99 });
const userAgents = () => ({ browsers: [{ type: 'Chrome', value: 8000 }], spiders: [{ type: 'Googlebot', value: 300 }], operatingSystems: [{ type: 'Windows', value: 6000 }], coverage: { browserRate: 0.9, osRate: 0.92, spiderRate: 0.03 } });
const domain = (d: string, n: number) => ({ domain: d, isPartial: false, ...kpis(n), statusBreakdown: statuses, durationBuckets: buckets, userAgents: userAgents(), modes: modes() });
const base = () => ({ asOf: now(), from: now() - 3600, to: now(), grain: 'minute', source: 'gateway_traffic_contribution', coverage: { from: now() - 3600, to: now(), isComplete: true, state: 'complete', missingBuckets: 0, windowBuckets: 60 } });
const points = () => Array.from({ length: 20 }, (_, i) => ({ bucketStartAt: now() - (20 - i) * 120, isPartial: false, ...kpis(200 + (i % 7) * 40), statusBreakdown: statuses, durationBuckets: buckets, modes: modes() }));

export const analyticsOverview = () => ({
  ...base(),
  overview: {
    currentInFlight: 12, dataLagSeconds: 4, hasData: true, isPartial: false, activeHostCount: 3, kpis: kpis(),
    statusBreakdown: statuses, durationBuckets: buckets, errorBreakdown: [{ type: 'upstream_error', value: 90 }],
    userAgents: userAgents(), topDomains: [domain('shop.example.com', 7000)], modes: modes(),
  },
  domains: [domain('shop.example.com', 7000)],
  points: points(),
});

export const analyticsTimeseries = () => ({ ...base(), domain: 'shop.example.com', points: points() });

export const analyticsContributions = () => ({
  ...base(),
  summary: { totalRequests: 12345, activeHostCount: 3, topHostName: 'shop.example.com', topHostRequestCounts: 7000 },
  rankings: [{ hostName: 'shop.example.com', requestCounts: 7000, inboundBytes: 5600000, outboundBytes: 37800000, totalBytes: 43400000, share: 0.57 }],
  timeline: Array.from({ length: 10 }, (_, i) => ({ period: now() - (10 - i) * 180, hostName: 'shop.example.com', requestCounts: 100 + i * 5, isPartial: false })),
});

export const gatewayDiagnostics = () => ({
  asOf: now(),
  health: { level: 'healthy', reasons: [], totalNodes: 2, activeNodes: 2, staleNodes: 0, drainingNodes: 0, latestHeartbeat: now() },
  runtime: { asOf: now(), heapAllocBytes: 120e6, heapInuseBytes: 150e6, residentBytes: 300e6, goroutines: 420, activeRequests: 12, queues: { history: { depth: 3, batchDepth: 1, dropped: 0 } } },
});

export const databaseDiagnostics = () => ({
  asOf: now(),
  health: { level: 'healthy', reasons: [] },
  pool: { total: 10, acquired: 3, idle: 7, constructing: 0, max: 20, usageRate: 0.15 },
  database: { name: 'yaginx', connections: 12, commits: 1000, rollbacks: 3, blocksRead: 50, blocksHit: 950, cacheHitRate: 0.95, tempFiles: 0, tempBytes: 0, deadlocks: 0, blockReadMs: 0, blockWriteMs: 0, statsResetAt: now() - 86400 },
  activity: { active: 2, idle: 8, idleInTransaction: 0, lockWaits: 0, ungrantedLocks: 0, maxLockWaitSeconds: 0 },
  io: { available: true, reads: 1, writes: 1, extends: 0, fsyncs: 0, evictions: 0, reuses: 0, readMs: 0, writeMs: 0 },
  tables: [{ name: 'request_history', liveTuples: 9000, deadTuples: 10, deadTupleRatio: 0.001, sizeBytes: 5e6, lastAutovacuumAt: now() - 3600, lastAutoanalyzeAt: now() - 3000 }],
  capabilities: {
    pgStatStatements: { enabled: true, installed: true, queryable: true, reason: '' },
    pgStatIO: { enabled: true, installed: true, queryable: true, reason: '' },
    trackIOTiming: { enabled: false, installed: true, queryable: true, reason: 'off' },
  },
  topSql: [{ queryId: '1001', query: 'SELECT 1', calls: 10, totalPlanMs: 1, totalExecMs: 8, meanPlanMs: 0.1, meanExecMs: 0.8, sharedBlocksRead: 1, sharedBlocksHit: 9, tempBlocksRead: 0, tempBlocksWritten: 0 }],
});

export const requestHistory = Array.from({ length: 12 }, (_, i) => ({
  id: String(9000 + i), requestAt: now() - i * 60, durationMs: 12 + i, clientIp: `203.0.113.${i + 1}`, method: 'GET',
  host: 'shop.example.com', path: `/api/items/${i}`, statusCode: i === 0 ? 502 : 200, upstreamStatusCode: i === 0 ? null : 200,
  statusSource: i === 0 ? 'gateway' : 'upstream', errorCategory: i === 0 ? 'upstream_error' : '', errorCode: '', userAgent: 'Mozilla/5.0',
  proxyStage: 'upstream', exceptionStage: i === 0 ? 'dial' : '', forwarderError: i === 0 ? 'connection refused' : '', hasException: i === 0, exceptionType: i === 0 ? 'DialError' : '',
}));

export const requestHistoryDetail = () => ({
  ...requestHistory[0], scheme: 'https', queryString: 'a=1', url: 'https://shop.example.com/api/items/0?a=1', protocol: 'HTTP/2',
  requestHeaders: { Accept: ['*/*'] }, responseHeaders: { 'Content-Type': ['application/json'] },
  requestBodyBase64: '', responseBodyBase64: Buffer.from('{"ok":true}').toString('base64'), requestContentType: '', responseContentType: 'application/json',
  requestBodyTruncated: false, responseBodyTruncated: false, exceptionMessage: 'dial tcp: connection refused', exceptionStackTrace: '',
});

export const ipProfile = (ip: string) => ({ ip, country: { name: '美国', isoCode: 'US' }, city: { name: 'Ashburn' }, asn: { number: 14618, organization: 'Amazon', network: '3.0.0.0/8' }, network: { type: 'datacenter', isp: 'AWS' }, traits: {} });

export const ipBlacklist = [
  { id: '7001', cidr: '198.51.100.0/24', scope: 'gateway', profile: ipProfile('198.51.100.7'), blockedCount: 1280, lastBlockedAt: now() - 30, createdAt: now() - 86400, isActive: true },
];

export const nodes = [
  { nodeId: 'n-1', displayName: 'edge-1', status: 'active', nodeNumber: 1, softwareVersion: '1.4.2', appliedRevision: '12', restartGeneration: '3', observedRestartGeneration: '3', lastHeartbeatAt: now(), stale: false },
  { nodeId: 'n-2', displayName: '', status: 'disabled', nodeNumber: null, softwareVersion: '1.4.0', appliedRevision: '9', restartGeneration: '1', observedRestartGeneration: '1', lastHeartbeatAt: null, stale: false },
];

export const credentials = [
  { id: 'c1', name: '生产 MCP', purpose: 'mcp', tokenPrefix: 'ygx_ab12', status: 'active', createdAt: now() - 86400, updatedAt: now() },
  { id: 'c2', name: '旧凭据', purpose: 'mcp', tokenPrefix: 'ygx_cd34', status: 'revoked', createdAt: now() - 864000, updatedAt: now() - 86400 },
];

export const acmeOrders = [
  { id: 'o1', domain: 'a.example.com', status: 'valid', attempt: 1, errorMessage: '', updatedAt: new Date().toISOString() },
  { id: 'o2', domain: 'b.example.com', status: 'failed', attempt: 3, errorMessage: 'dns challenge timeout', updatedAt: new Date().toISOString() },
];

export const resourceHost = (host: string, managed: boolean) => ({
  host, managementStatus: managed ? 'managed' : 'unmanaged', domainConfig: null,
  websites: managed ? [{ id: '5001', name: 'shop' }] : [],
  certificate: managed ? { fingerprint: 'AB:CD', notBefore: now() - 86400, notAfter: now() + 80 * 86400, isActive: true } : null,
  lastActivity: now() - 120,
});

export const subjectView = () => ({
  subjectType: 'domain', website: null, hosts: [resourceHost('shop.example.com', true)],
  overview: { ...base(), ...analyticsOverview().overview }, timeseries: { ...base(), domain: 'shop.example.com', points: points() },
});
