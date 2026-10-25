/**
 * Proxy
 * 本地反向代理
 */
import type { ProxyOptions } from 'vite';

// 开发服务器把 /api 代理到这里；默认是 https://goya.feinian.net/，可用 .env 或环境变量 VITE_PROXY_HTTP 覆盖。
// 只在 Vite 配置（Node 环境）里读取，不能放进会打进浏览器包的 constant.ts。
const DEFAULT_PROXY_HTTP = 'https://goya.feinian.net/';

type ProxyTargetList = Record<
  string,
  // eslint-disable-next-line no-unused-vars
  ProxyOptions & { rewrite?: (path: string) => string }
>;

const httpsRE = /^https:\/\//;

/**
 * Generate proxy
 * @param list
 */
export function createProxy(target: string = DEFAULT_PROXY_HTTP) {
  const ProxyList: ProxyTargetList = {
    "/api": {
      target,
      changeOrigin: true,
      //  rewrite: (pre) => pre.replace(/^\/api/, ''), // 将 /api 重写为空
      // https is require secure=false
      ...(httpsRE.test(target) ? { secure: true } : {}),
    },
  };
  return ProxyList;
}
