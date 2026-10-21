/**
 * Proxy
 * 本地反向代理
 */
import type { ProxyOptions } from 'vite';

// 开发服务器把 /api 代理到这里；默认是本机网关的管理端口，可用环境变量 VITE_PROXY_HTTP 覆盖。
// 只在 Vite 配置（Node 环境）里读取，不能放进会打进浏览器包的 constant.ts。
const VITE_PROXY_HTTP = process.env.VITE_PROXY_HTTP || 'http://127.0.0.1:9090/';

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
export function createProxy() {
  const ProxyList: ProxyTargetList = {
    "/api": {
      target: VITE_PROXY_HTTP,
      changeOrigin: true,
      //  rewrite: (pre) => pre.replace(/^\/api/, ''), // 将 /api 重写为空
      // https is require secure=false
      ...(httpsRE.test(VITE_PROXY_HTTP) ? { secure: true } : {}),
    },
  };
  return ProxyList;
}
