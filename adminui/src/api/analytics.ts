import { FetchJson } from "@/requests/FetchJson";
import type { IApiRspEnvelop, Page, PageRequest } from "./apiTypes";
import { requireInt64String } from "./identifier";
import type { DomainListItem, Int64, Website } from "./website";

export interface IMetricItem {
  type: string;
  value: number;
}

export interface IAnalyticsKpis {
  requests: number;
  requestsPerSecond: number;
  inboundBytes: number;
  outboundBytes: number;
  inboundBytesPerSecond: number;
  outboundBytesPerSecond: number;
  errorRequests: number;
  errorRate: number;
  p50Ms: number | null;
  p95Ms: number | null;
  p99Ms: number | null;
  avgDurationMs: number;
  maxDurationMs: number;
  latencyCoverageRate: number;
}

export interface IAnalyticsLatency {
  p50Ms: number | null;
  p95Ms: number | null;
  p99Ms: number | null;
  avgMs: number;
}

export interface IAnalyticsModeMetrics {
  requests: number;
  completionLatency: IAnalyticsLatency;
  firstByteLatency: IAnalyticsLatency;
  firstTokenLatency: IAnalyticsLatency;
  completionCoverageRate: number;
  firstByteCoverageRate: number;
  firstTokenCoverageRate: number;
}

export interface IAnalyticsModeBreakdown {
  nonStream: IAnalyticsModeMetrics;
  stream: IAnalyticsModeMetrics;
  upgrade: IAnalyticsModeMetrics;
}

export interface IAnalyticsUserAgents {
  browsers: IMetricItem[];
  spiders: IMetricItem[];
  operatingSystems: IMetricItem[];
  coverage: {
    browserRate: number;
    osRate: number;
    spiderRate: number;
  };
}

export interface IAnalyticsCoverage {
  from: number;
  to: number;
  isComplete: boolean;
  state: "collecting" | "complete" | "incomplete";
  missingBuckets: number;
  windowBuckets: number;
}

export interface IAnalyticsDomain extends IAnalyticsKpis {
  domain: string;
  isPartial: boolean;
  statusBreakdown: IMetricItem[];
  durationBuckets: IMetricItem[];
  userAgents: IAnalyticsUserAgents;
  modes?: IAnalyticsModeBreakdown;
}

export interface IAnalyticsPoint extends IAnalyticsKpis {
  bucketStartAt: number;
  isPartial: boolean;
  statusBreakdown: IMetricItem[];
  durationBuckets: IMetricItem[];
  modes?: IAnalyticsModeBreakdown;
}

interface IAnalyticsBase {
  asOf: number;
  from: number;
  to: number;
  grain: "minute" | "hour" | "day";
  source: "gateway_traffic_contribution" | "resource_report" | "gateway_traffic_contribution+resource_report";
  coverage: IAnalyticsCoverage;
}

export interface IAnalyticsOverview extends IAnalyticsBase {
  currentInFlight: number;
  dataLagSeconds: number;
  hasData: boolean;
  isPartial: boolean;
  activeHostCount: number;
  kpis: IAnalyticsKpis;
  statusBreakdown: IMetricItem[];
  durationBuckets: IMetricItem[];
  errorBreakdown: IMetricItem[];
  userAgents: IAnalyticsUserAgents;
  topDomains: IAnalyticsDomain[];
  modes: IAnalyticsModeBreakdown;
}

export interface IAnalyticsDomains extends IAnalyticsBase {
  domains: IAnalyticsDomain[];
}

export interface IAnalyticsTimeseries extends IAnalyticsBase {
  domain: string;
  points: IAnalyticsPoint[];
}

export interface IAnalyticsHostRanking {
  hostName: string;
  requestCounts: number;
  inboundBytes: number;
  outboundBytes: number;
  totalBytes: number;
  share: number;
}

export interface IAnalyticsContributionPoint {
  period: number;
  hostName: string;
  requestCounts: number;
  isPartial: boolean;
}

export interface IAnalyticsContributions extends IAnalyticsBase {
  summary: {
    totalRequests: number;
    activeHostCount: number;
    topHostName: string;
    topHostRequestCounts: number;
  };
  rankings: IAnalyticsHostRanking[];
  timeline: IAnalyticsContributionPoint[];
}

export interface IAnalyticsDetail {
  id: string;
  hostName: string;
  period: number;
  requestCounts: number;
  inboundBytes: number;
  outboundBytes: number;
  totalBytes: number;
  errorRequests: number;
  errorRate: number;
  avgDurationMs: number;
  p95Ms: number | null;
  isPartial: boolean;
}

export type IAnalyticsDetails = IAnalyticsBase & Page<IAnalyticsDetail>;

export type AnalyticsOverviewSection = "overview" | "domains" | "timeseries";

export interface IAnalyticsOverviewResult extends IAnalyticsBase {
  overview?: Omit<IAnalyticsOverview, keyof IAnalyticsBase>;
  domains?: IAnalyticsDomain[];
  points?: IAnalyticsPoint[];
}

// The overview response carries only the sections that were asked for, so a
// caller that wants the summary has to flatten the shared envelope back in.
export const overviewSection = (result: IAnalyticsOverviewResult): IAnalyticsOverview | undefined =>
  result.overview ? { ...result, ...result.overview } : undefined;

// AnalyticsQuery is the one request contract every analytics operation accepts.
// An omitted window falls back to the service default and an omitted domain
// aggregates every Host.
export type AnalyticsQuery = { from?: number; to?: number; domain?: string; domains?: string[] };
type RequestOptions = { ignoreGlobalError?: boolean; signal?: AbortSignal };

export const getAnalyticsOverview = (
  data: AnalyticsQuery,
  sections: AnalyticsOverviewSection[],
  options: RequestOptions = {}
): Promise<IApiRspEnvelop<IAnalyticsOverviewResult>> =>
  FetchJson({ url: "/api/analytics/overview", method: "POST", data: { ...data, sections }, ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

export const getAnalyticsDomains = (data: AnalyticsQuery, options: RequestOptions = {}): Promise<IApiRspEnvelop<IAnalyticsDomains>> =>
  FetchJson({ url: "/api/analytics/domains", method: "POST", data, ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

export const getAnalyticsTimeseries = (data: AnalyticsQuery, options: RequestOptions = {}): Promise<IApiRspEnvelop<IAnalyticsTimeseries>> =>
  FetchJson({ url: "/api/analytics/timeseries", method: "POST", data, ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

export const getAnalyticsContributions = (data: AnalyticsQuery, options: RequestOptions = {}): Promise<IApiRspEnvelop<IAnalyticsContributions>> =>
  FetchJson({ url: "/api/analytics/contributions", method: "POST", data, ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

export const getAnalyticsDetails = (data: AnalyticsQuery & PageRequest, options: RequestOptions = {}): Promise<IApiRspEnvelop<IAnalyticsDetails>> =>
  FetchJson({ url: "/api/analytics/details", method: "POST", data, ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

export type DatabaseHealthLevel = "healthy" | "warning" | "critical";

export interface IDatabaseCapability {
  enabled: boolean;
  installed: boolean;
  queryable: boolean;
  reason: string;
}

export interface IDatabasePerformance {
  asOf: number;
  health: {
    level: DatabaseHealthLevel;
    reasons: string[];
  };
  pool: {
    total: number;
    acquired: number;
    idle: number;
    constructing: number;
    max: number;
    usageRate: number;
  };
  database: {
    name: string;
    connections: number;
    commits: number;
    rollbacks: number;
    blocksRead: number;
    blocksHit: number;
    cacheHitRate: number;
    tempFiles: number;
    tempBytes: number;
    deadlocks: number;
    blockReadMs: number;
    blockWriteMs: number;
    statsResetAt: number;
  };
  activity: {
    active: number;
    idle: number;
    idleInTransaction: number;
    lockWaits: number;
    ungrantedLocks: number;
    maxLockWaitSeconds: number;
  };
  io: {
    available: boolean;
    reads?: number;
    writes?: number;
    extends?: number;
    fsyncs?: number;
    evictions?: number;
    reuses?: number;
    readMs?: number;
    writeMs?: number;
  };
  tables: Array<{
    name: string;
    liveTuples: number;
    deadTuples: number;
    deadTupleRatio: number;
    sizeBytes: number;
    lastAutovacuumAt: number;
    lastAutoanalyzeAt: number;
  }>;
  capabilities: {
    pgStatStatements: IDatabaseCapability;
    pgStatIO: IDatabaseCapability;
    trackIOTiming: IDatabaseCapability;
  };
  topSql: Array<{
    queryId: string;
    query: string;
    calls: number;
    totalPlanMs: number;
    totalExecMs: number;
    meanPlanMs: number;
    meanExecMs: number;
    sharedBlocksRead: number;
    sharedBlocksHit: number;
    tempBlocksRead: number;
    tempBlocksWritten: number;
  }>;
}

export const getDatabasePerformance = (options: RequestOptions = {}): Promise<IApiRspEnvelop<IDatabasePerformance>> =>
  FetchJson({ url: "/api/diagnostics/database", method: "GET", ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

export interface IGatewayRuntimeQueue { depth: number; batchDepth: number; dropped: number; }
export interface IGatewayDiagnosticsOverview {
  asOf: number;
  health: { level: "healthy" | "warning" | "critical"; reasons: string[]; totalNodes: number; activeNodes: number; staleNodes: number; drainingNodes: number; latestHeartbeat: number };
  runtime: { asOf: number; heapAllocBytes: number; heapInuseBytes: number; residentBytes: number; goroutines: number; activeRequests: number; queues: Record<string, IGatewayRuntimeQueue> };
}

export const getGatewayDiagnosticsOverview = (options: RequestOptions = {}): Promise<IApiRspEnvelop<IGatewayDiagnosticsOverview>> =>
  FetchJson({ url: "/api/diagnostics/overview", method: "GET", ignoreGlobalError: options.ignoreGlobalError, signal: options.signal });

// The Host directory and the per-subject read model are analytics too: they
// answer "what is this Host doing" with the same KPI and timeseries shapes as
// the aggregate operations above.

export interface IResourceCertificate {
  fingerprint: string;
  notBefore: number;
  notAfter: number;
  isActive: boolean;
}

export interface IResourceWebsiteRef { id: Int64; name: string; }

export interface IResourceHost {
  host: string;
  managementStatus: "managed" | "unmanaged";
  domainConfig?: DomainListItem | null;
  websites: IResourceWebsiteRef[];
  certificate?: IResourceCertificate | null;
  lastActivity: number;
}

export type IResourceCatalog = Page<IResourceHost> & {
  managedTotal: number;
  unmanagedTotal: number;
};

export type ResourceCatalogStatus = "managed" | "unmanaged";
export type ResourceCatalogWebsiteAssociation = "associated" | "unassociated";

export interface IResourceCatalogQuery extends PageRequest {
  host?: string;
  status?: ResourceCatalogStatus;
  websiteAssociation?: ResourceCatalogWebsiteAssociation;
}

export type AnalyticsSubjectType = "domain" | "website";

// One subject's configuration and analytics read model. website is null for a
// domain subject, and hosts then carries the single matching Host.
export interface IAnalyticsSubjectView {
  subjectType: AnalyticsSubjectType;
  website: Website | null;
  hosts: IResourceHost[];
  overview: IAnalyticsOverview;
  timeseries: IAnalyticsTimeseries;
}

const validateWebsiteIDs = (hosts: IResourceHost[]): void => {
  for (const host of hosts) {
    for (const website of host.websites) {
      requireInt64String(website.id, "关联网站 ID");
    }
  }
};

export const getResourceCatalog = async (params: IResourceCatalogQuery): Promise<IApiRspEnvelop<IResourceCatalog>> => {
  const response = await FetchJson<IResourceCatalog>({ url: "/api/analytics/catalog", method: "GET", params });
  if (response.code !== 200) return response;
  validateWebsiteIDs(response.data.records);
  return response;
};

const getAnalyticsSubject = async (
  params: AnalyticsQuery & { subjectType: AnalyticsSubjectType; key: string }
): Promise<IApiRspEnvelop<IAnalyticsSubjectView | null>> => {
  const response = await FetchJson<IAnalyticsSubjectView | null>({ url: "/api/analytics/subject", method: "GET", params });
  if (response.code !== 200 || !response.data) return response;
  if (response.data.website) requireInt64String(response.data.website.id, "网站 ID");
  validateWebsiteIDs(response.data.hosts);
  return response;
};

export const getDomainAnalytics = (params: AnalyticsQuery & { host: string }): Promise<IApiRspEnvelop<IAnalyticsSubjectView | null>> =>
  getAnalyticsSubject({ from: params.from, to: params.to, subjectType: "domain", key: params.host });

export const getWebsiteAnalytics = (params: AnalyticsQuery & { id: Int64 }): Promise<IApiRspEnvelop<IAnalyticsSubjectView | null>> =>
  getAnalyticsSubject({ from: params.from, to: params.to, subjectType: "website", key: String(params.id) });
