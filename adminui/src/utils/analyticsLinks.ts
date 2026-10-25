// 分析相关页面的统一跳转地址：站点为主轴，Host 与请求追踪的筛选参数统一为 host / websiteId / from / to。
export interface AnalyticsWindow {
  from?: number;
  to?: number;
}

type QueryValue = string | number | null | undefined;

const withQuery = (path: string, params: Record<string, QueryValue>): string => {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') query.set(key, String(value));
  });
  const text = query.toString();
  return text ? `${path}?${text}` : path;
};

export const websiteAnalyticsPath = (id: string | number, tab?: string): string =>
  withQuery(`/website/${encodeURIComponent(String(id))}/analytics`, { tab });

export const hostDetailPath = (host: string): string => `/resource/hosts/view/${encodeURIComponent(host)}`;

export const requestHistoryPath = (filters: Record<string, QueryValue> = {}): string =>
  withQuery('/monitor/requestHistory/list', filters);

export const acmeOrdersPath = (domain: string): string => withQuery('/systems/acme', { domain });
