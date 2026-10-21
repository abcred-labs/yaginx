import { test as base, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import * as fx from './fixtures';

export interface RecordedRequest {
  method: string;
  path: string;
  body: any;
}

type Handler = (body: any, url: URL) => unknown;

const jsonOrNull = (text: string | null) => {
  if (!text) return null;
  try { return JSON.parse(text); } catch { return text; }
};

const defaultRoutes = (): Record<string, Handler> => ({
  'GET /api/account/refresh_token': () => fx.loginData,
  'POST /api/account/login': () => fx.loginData,
  'GET /api/account/profile': () => ({ email: 'admin@yaginx.local' }),
  'GET /api/account/sessions': () => [{ id: 's1', userAgent: 'Chrome', clientIp: '10.0.0.8', lastUsedAt: new Date().toISOString(), current: true, remembered: true }],
  'GET /api/system_setting/runtime': () => ({ config: fx.runtimeConfig(), revision: '12', updatedAt: 1, applyMode: 'restart' }),
  'POST /api/system_setting/runtime': () => true,
  'GET /api/system_setting/runtime/status': () => ({ revision: '12', pendingRestart: false, nodes: [], applyMode: 'restart' }),
  'GET /api/system_setting/runtime/access-context': () => ({ clientIP: '203.0.113.9', allowedCIDRs: ['127.0.0.1/32'], clientAllowed: false }),
  'GET /api/system_setting/smtp': () => ({ enabled: true, host: 'smtp.example.com', port: 465, useSsl: true, userName: 'mailer', password: '', fromEmail: 'noreply@example.com', fromName: 'Yaginx' }),
  'POST /api/gateway_node/search': () => ({ records: fx.nodes, paging: { pageIndex: 1, pageSize: 200, total: fx.nodes.length } }),
  'GET /api/acme/orders': () => ({ records: fx.acmeOrders, paging: { pageIndex: 1, pageSize: 200, total: fx.acmeOrders.length } }),
  'GET /api/acme/logs': () => emptyPage(100),
  'GET /api/system_credentials': () => fx.credentials,
  'POST /api/system_credentials': () => ({ id: 'c3', token: 'ygx_secret_token_value', tokenPrefix: 'ygx_ef56' }),
  'POST /api/website/search': () => ({ records: [fx.website('5001', 'shop'), fx.website('5002', 'blog')], paging: { pageIndex: 1, pageSize: 50, total: 2 } }),
  'GET /api/website/get': () => fx.website(),
  'POST /api/website/upsert': () => fx.website(),
  'POST /api/request_history/search': () => ({ records: fx.requestHistory, paging: { pageIndex: 1, pageSize: 50, total: fx.requestHistory.length } }),
  'GET /api/request_history/get': () => fx.requestHistoryDetail(),
  'POST /api/ip_blacklist/search': () => ({ records: fx.ipBlacklist, paging: { pageIndex: 1, pageSize: 50, total: fx.ipBlacklist.length } }),
  'POST /api/analytics/overview': () => fx.analyticsOverview(),
  'POST /api/analytics/timeseries': () => fx.analyticsTimeseries(),
  'POST /api/analytics/contributions': () => fx.analyticsContributions(),
  'GET /api/analytics/catalog': () => ({ records: [fx.resourceHost('shop.example.com', true), fx.resourceHost('unknown.example.net', false)], paging: { pageIndex: 1, pageSize: 50, total: 2 }, managedTotal: 1, unmanagedTotal: 1 }),
  'GET /api/analytics/subject': () => fx.subjectView(),
  'GET /api/diagnostics/overview': () => fx.gatewayDiagnostics(),
  'GET /api/diagnostics/database': () => fx.databaseDiagnostics(),
});

const emptyPage = (pageSize: number) => ({ records: [], paging: { pageIndex: 1, pageSize, total: 0 } });

export interface Api {
  /** 所有非 GET 请求，按发生顺序。 */
  writes: RecordedRequest[];
  /** 最近一次对该路径的写请求体。 */
  lastWrite: (path: string) => any;
  /** 覆盖某个接口的返回内容，键形如 `POST /api/website/upsert`。 */
  override: (key: string, handler: Handler) => void;
}

/** 拦截 /api/**：未知接口返回空分页，避免请求落到占位的开发代理上。 */
export const mockApi = async (page: Page): Promise<Api> => {
  const routes = defaultRoutes();
  const writes: RecordedRequest[] = [];
  await page.route((url) => url.pathname.startsWith('/api/'), async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const body = jsonOrNull(request.postData());
    if (request.method() !== 'GET') writes.push({ method: request.method(), path: url.pathname, body });
    const key = `${request.method()} ${url.pathname}`;
    let handler = routes[key];
    if (!handler && url.pathname.startsWith('/api/acme/orders/') && url.pathname.endsWith('/logs')) handler = () => emptyPage(10);
    if (!handler && request.method() !== 'GET') handler = () => true;
    const data = handler ? handler(body, url) : emptyPage(50);
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fx.envelope(data)) });
  });
  return {
    writes,
    lastWrite: (path) => [...writes].reverse().find((item) => item.path === path)?.body,
    override: (key, handler) => { routes[key] = handler; },
  };
};

/** 预置“已登录”状态：令牌只在内存里，刷新会走 mock 的 refresh_token。 */
export const signIn = async (page: Page) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('auth_info', JSON.stringify({
      state: { isCheckAuthLoopEnable: true, isLoginSuccess: true, userId: '1', name: 'admin@yaginx.local' },
      version: 1,
    }));
  });
};

interface Fixtures {
  api: Api;
  /** 页面运行期间的控制台错误与未捕获异常，用例结束时必须为空。 */
  problems: string[];
}

export const test = base.extend<Fixtures>({
  problems: [async ({ page }, use) => {
    const problems: string[] = [];
    page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
    page.on('console', (message) => {
      if (message.type() === 'error') problems.push(`console: ${message.text()}`);
    });
    await use(problems);
    expect(problems, '页面不应出现控制台错误或未捕获异常').toEqual([]);
  }, { auto: true }],
  api: [async ({ page, problems }, use) => {
    void problems;
    await use(await mockApi(page));
  }, { auto: true }],
});

export { expect };
