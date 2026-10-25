import { FetchJson } from "@/requests/FetchJson";
import type { IApiRspEnvelop, Page, PageRequest } from "./apiTypes";
import { requireInt64String } from "./identifier";
import { validateRecordList, validateResponse } from "./apiResponse";
import type { Int64 } from "./identifier";
import type { components } from "./generated/management";

type ManagementSchema = components["schemas"];

export type { Int64 } from "./identifier";

export interface KeyValueString {
  key: string;
  value: string;
}

export interface ProxyRule {
  ruleId?: Int64;
  name?: string | null;
  description?: string | null;
  isEnabled: boolean;
  pathPattern?: string | null;
  isRegexMatchPattern?: boolean;
  destination?: string | null;
  destinationHost?: string | null;
  preserveOriginalHost?: boolean;
  allowInvalidTlsCertificate?: boolean | null;
  webProxy?: string | null;
  removePrefix?: string | null;
  upstreamPoolId?: string;
  isFallback?: boolean;
  /** UI-only discriminator; converted to destination/upstreamPoolId before save. */
  targetType?: 'direct' | 'pool';
}

export interface UpstreamEndpoint {
  address: string;
  host?: string;
  weight: number;
  priority?: number;
  enabled: boolean;
  tlsVerify?: boolean;
  proxy?: string;
}

export interface UpstreamPool {
  id: string;
  name: string;
  strategy: 'weightedRoundRobin' | 'leastConnections' | 'random' | 'failover';
  endpoints: UpstreamEndpoint[];
}

export interface WebsiteHost {
  id?: Int64;
  hostname: string;
  enabled: boolean;
  role: 'primary' | 'alias';
  accessMode: 'proxy' | 'redirect';
  redirectStatusCode?: 301 | 308;
  redirectTargetHost?: string | null;
  /** Whether ACME auto-renewal is enabled for this host; replaces the old existing/request/manual tri-state. */
  certificateMode?: 'request' | 'manual';
  certificateId?: Int64 | null;
  verificationStatus?: 'unknown' | 'pending' | 'verified' | 'failed';
}

export interface AcmeRequestResult {
  status: string;
  message: string;
}

export interface AcmeStatusResult {
  hostname: string;
  certificate?: HostCertificateStatus | null;
  acmeOrder?: HostAcmeOrderStatus | null;
  checkedAt: string;
  hasOrder: boolean;
  orderStatus?: string;
  errorMessage?: string;
  attempt?: number;
  nextAttemptAt?: string | null;
  certificateNotAfter?: string | null;
}

export interface HostCertificateStatus {
  exists: boolean;
  validity: 'valid' | 'expired' | 'not_yet_valid' | 'absent';
  isActive: boolean;
  notBefore: string;
  notAfter: string;
  remainingSeconds: number;
  remainingDays: number;
  fingerprint: string;
  issuer: string;
  serialNumber: string;
  dnsNames: string[];
  source: 'acme' | 'imported_pfx' | string;
}

export interface HostAcmeOrderStatus {
  exists: boolean;
  isInProgress: boolean;
  id: Int64 | null;
  status: string;
  attempt: number;
  createdAt: string;
  updatedAt: string;
  nextAttemptAt?: string | null;
  finishedAt?: string | null;
  errorClass: string;
  errorMessage: string;
}

export interface SimpleResponse {
  path: string;
  contentType?: string | null;
  statusCode?: number;
  statusFamily?: string;
  responseContent?: string | null;
  extraHeaders?: Array<{ key?: string; value?: string }>;
}

export interface WebsiteRequestHistoryPolicy {
  mode: "inherit" | "errorsOnly" | "all";
  successSamplingRate?: number | null;
}

export interface WebsiteUpsertInput {
  id?: Int64;
  name?: string;
  defaultHost?: string | null;
  defaultDestination?: string | null;
  defaultDestinationHost?: string | null;
  webProxy?: string | null;
  authorizationPolicy?: string | null;
  preserveOriginalHost?: boolean;
  allowInvalidTlsCertificate?: boolean;
  redirectHttpToHttps?: boolean;
  isMaintenanceMode?: boolean;
  ipAccessMode?: "disabled" | "blacklist" | "whitelist";
  ipAccessCIDRs?: string[];
  hosts?: string[];
  hostBindings?: WebsiteHost[];
  upstreamPools?: UpstreamPool[];
  proxyRules?: ProxyRule[];
  proxyTransforms?: KeyValueString[];
  simpleResponses?: SimpleResponse[];
  requestHistory?: WebsiteRequestHistoryPolicy | null;
  /** Per-site upstream response header timeout (Go duration, e.g. "600s"); empty inherits the global setting. */
  responseHeaderTimeout?: string;
}

export interface Website extends Omit<WebsiteUpsertInput, "id" | "name"> {
  id: Int64;
  name: string;
  hasCertificate: boolean;
}

export interface CertificateSummary {
  hasCertificate: boolean;
  notBefore: number;
  notAfter: number;
  thumbprint: string | null;
}

export interface DomainUpsertInput {
  id?: Int64;
  name: string;
  autoRequestCertificate?: boolean;
  isVerified?: boolean;
  freeCertMessage?: string | null;
}

export interface Domain extends Required<Omit<DomainUpsertInput, "id">> {
  id: Int64;
}

export interface DomainListItem extends Domain {
  certSpec: CertificateSummary;
}

export interface IWebsiteSearchRequest extends PageRequest {
  domain?: string;
  upstream?: string;
  hasCertificate?: boolean;
}

export interface IWebsiteBatchDeleteResult {
  deletedIds: Int64[];
  notFoundIds: Int64[];
}

type WebsiteRequestOptions = { signal?: AbortSignal };

const validateWebsiteID = (website: Website): Website => {
  requireInt64String(website.id, "网站 ID");
  for (const rule of website.proxyRules ?? []) {
    if (rule.ruleId !== undefined) {
      requireInt64String(rule.ruleId, "转发规则 ID");
    }
  }
  return website;
};

export const websiteSearch = async (data: IWebsiteSearchRequest): Promise<IApiRspEnvelop<Page<Website>>> => {
  const response = await FetchJson<Page<Website>>({ url: '/api/website/search', method: 'POST', data });
  return validateRecordList(response, validateWebsiteID);
};
export const websiteGet = async (data: { id: Int64 }, options: WebsiteRequestOptions = {}): Promise<IApiRspEnvelop<Website | null>> => {
  const response = await FetchJson<Website | null>({ url: '/api/website/get', method: 'GET', params: data, signal: options.signal });
  return validateResponse(response, (value) => value ? validateWebsiteID(value) : value);
};
export const websiteUpsert = async (data: WebsiteUpsertInput): Promise<IApiRspEnvelop<Website>> => {
  const response = await FetchJson<Website>({ url: '/api/website/upsert', method: 'POST', data });
  return validateResponse(response, validateWebsiteID);
};
export const websiteResponseID = (response: unknown): Int64 | null => {
  if (!response || typeof response !== 'object' || Array.isArray(response)) return null;
  const envelope = response as { code?: unknown; data?: unknown };
  if (envelope.code !== 200 || !envelope.data || typeof envelope.data !== 'object' || Array.isArray(envelope.data)) return null;
  const id = (envelope.data as { id?: unknown }).id;
  return typeof id === 'string' && /^[1-9]\d*$/.test(id) ? id : null;
};
export const websiteDelete = (data: { id: Int64 }): Promise<IApiRspEnvelop<Record<string, never>>> => FetchJson({ url: '/api/website/delete', method: 'DELETE', params: data });
export const websiteBatchDelete = async (data: { ids: Int64[] }): Promise<IApiRspEnvelop<IWebsiteBatchDeleteResult>> => {
  const response = await FetchJson<IWebsiteBatchDeleteResult>({ url: '/api/website/delete_batch', method: 'POST', data });
  return validateResponse(response, (value) => {
    value.deletedIds.forEach((id) => requireInt64String(id, "已删除网站 ID"));
    value.notFoundIds.forEach((id) => requireInt64String(id, "未找到网站 ID"));
    return value;
  });
};

const validateDomainID = <T extends { id: Int64 }>(domain: T): T => {
  requireInt64String(domain.id, "域名 ID");
  return domain;
};

export const webDomainSearch = async (data: PageRequest = {}): Promise<IApiRspEnvelop<Page<DomainListItem>>> => {
  const response = await FetchJson<Page<DomainListItem>>({ url: '/api/web_domain/search', method: 'POST', data });
  return validateRecordList(response, validateDomainID);
};
export const webDomainGet = async (data: { id: Int64 }): Promise<IApiRspEnvelop<DomainListItem | null>> => {
  const response = await FetchJson<DomainListItem | null>({ url: '/api/web_domain/get', method: 'GET', params: data });
  return validateResponse(response, (value) => value ? validateDomainID(value) : value);
};
export const webDomainUpsert = async (data: DomainUpsertInput): Promise<IApiRspEnvelop<Domain>> => {
  const response = await FetchJson<Domain>({ url: '/api/web_domain/upsert', method: 'POST', data });
  return validateResponse(response, validateDomainID);
};
export const webDomainDelete = (data: { id: Int64 } | { name: string }): Promise<IApiRspEnvelop<Record<string, never>>> => FetchJson({ url: '/api/web_domain/delete', method: 'DELETE', data });
export const webDomainRequestCertificate = (data: { name: string }): Promise<IApiRspEnvelop<AcmeRequestResult>> => FetchJson({ url: '/api/web_domain/acme/request', method: 'POST', data });
export const webDomainCertificateStatus = (data: { name: string }): Promise<IApiRspEnvelop<AcmeStatusResult>> => FetchJson({ url: '/api/web_domain/acme/status', method: 'GET', params: data });

const validateRequestHistoryID = <T extends { id: Int64 }>(record: T): T => {
  requireInt64String(record.id, "请求记录 ID");
  return record;
};

const validateIPBlacklistID = (record: IIPBlacklistRule): IIPBlacklistRule => {
  requireInt64String(record.id, "黑名单规则 ID");
  return record;
};

export const requestHistorySearch = async (data: IRequestHistorySearchRequest): Promise<IApiRspEnvelop<IPagedResult<IRequestHistoryListItem>>> => {
  const response = await FetchJson<IPagedResult<IRequestHistoryListItem>>({ url: '/api/request_history/search', method: 'POST', data });
  return validateRecordList(response, validateRequestHistoryID);
};
export const requestHistoryGet = async (data: { id: Int64 }): Promise<IApiRspEnvelop<IRequestHistoryDetail | null>> => {
  const response = await FetchJson<IRequestHistoryDetail | null>({ url: '/api/request_history/get', method: 'GET', params: data });
  return validateResponse(response, (value) => value ? validateRequestHistoryID(value) : value);
};
export const ipBlacklistSearch = async (data: IIPBlacklistSearchRequest): Promise<IApiRspEnvelop<IPagedResult<IIPBlacklistRule>>> => {
  const response = await FetchJson<IPagedResult<IIPBlacklistRule>>({ url: '/api/ip_blacklist/search', method: 'POST', data });
  return validateRecordList(response, validateIPBlacklistID);
};
export const ipBlacklistGet = async (data: { id: Int64 }): Promise<IApiRspEnvelop<IIPBlacklistRule | null>> => {
  const response = await FetchJson<IIPBlacklistRule | null>({ url: '/api/ip_blacklist/get', method: 'GET', params: data });
  return validateResponse(response, (value) => value ? validateIPBlacklistID(value) : value);
};
export const ipBlacklistUpsert = async (data: { cidr: string }): Promise<IApiRspEnvelop<IIPBlacklistRule>> => {
  const response = await FetchJson<IIPBlacklistRule>({ url: '/api/ip_blacklist/upsert', method: 'POST', data });
  return validateResponse(response, validateIPBlacklistID);
};
export const ipBlacklistRemove = async (data: { id: Int64 }): Promise<IApiRspEnvelop<IIPBlacklistRule>> => {
  const response = await FetchJson<IIPBlacklistRule>({ url: '/api/ip_blacklist/remove', method: 'POST', data });
  return validateResponse(response, validateIPBlacklistID);
};
export interface IIPBlacklistSummary { activeRules: number; blockedTotal: number }
export const ipBlacklistSummary = (options: { ignoreGlobalError?: boolean; signal?: AbortSignal } = {}): Promise<IApiRspEnvelop<IIPBlacklistSummary>> => FetchJson({ url: '/api/ip_blacklist/summary', method: 'GET', ...options });
export const ipBlacklistLookup = (data: { ip: string }): Promise<IApiRspEnvelop<IIPProfile>> => FetchJson({ url: '/api/ip_blacklist/lookup', method: 'GET', params: data });

export interface IRequestHistorySearchRequest extends PageRequest {
	websiteId?: Int64;
  from?: number;
  to?: number;
  host?: string;
  statusCode?: number;
  statusFamily?: string;
  statusCodePattern?: string;
  upstreamStatusCode?: number;
  statusSource?: string;
  errorCategory?: string;
  errorCode?: string;
  clientIp?: string;
  userAgent?: string;
}

export interface IRequestHistoryListItem {
  id: Int64;
  requestAt: number;
  durationMs: number;
  clientIp: string;
  method: string;
  host: string;
  path: string;
  statusCode: number;
  upstreamStatusCode?: number | null;
  statusSource: string;
  errorCategory: string;
  errorCode: string;
  userAgent: string;
  proxyStage: string;
  exceptionStage: string;
  forwarderError: string;
  hasException: boolean;
  exceptionType: string;
}

export interface IRequestHistoryDetail extends IRequestHistoryListItem {
  traceId?: string;
  scheme: string;
  queryString: string;
  url: string;
  protocol: string;
  requestHeaders: Record<string, string[]>;
  responseHeaders: Record<string, string[]>;
  requestBodyBase64: string;
  responseBodyBase64: string;
  requestContentType: string;
  responseContentType: string;
  requestBodyTruncated: boolean;
  responseBodyTruncated: boolean;
  exceptionMessage: string;
  exceptionStackTrace: string;
}

export type IPagedResult<T> = Page<T>;

export interface IIPProfileName {
  name?: string | null;
  isoCode?: string | null;
}

export interface IIPProfile {
  ip: string;
  country?: IIPProfileName | null;
  subdivision?: IIPProfileName | null;
  city?: { name?: string | null; geoNameId?: number | null } | null;
  asn?: { number?: number | null; organization?: string | null; network?: string | null } | null;
  network?: {
    type?: "datacenter" | "residential" | "mobile" | "proxy" | "unknown" | string;
    isp?: string | null;
    organization?: string | null;
    connectionType?: string | null;
    userType?: string | null;
  } | null;
  traits?: Record<string, boolean | null | undefined> | null;
}

export interface IIPBlacklistRule {
  id: Int64;
  cidr: string;
  profile: IIPProfile;
  blockedCount: number | string;
  lastBlockedAt?: number | string | null;
  createdAt: number | string;
  removedAt?: number | string | null;
  isActive: boolean;
}

export interface IIPBlacklistSearchRequest extends PageRequest {
  keyword?: string;
  includeRemoved?: boolean | null;
}
