import { notifyError } from "@/ui/notify";
import qs from "qs";
import { Fetch } from "./Fetch";
import { PREFIX } from "./requestConsts";
import { API_BASE_URL } from "../../config/constant";
import { isFirstPartyApiUrl, normalizeApiResponse } from "./responseNormalizer";
import type { IApiRspEnvelop } from "@/api/apiTypes";
import { useAccessStore } from "@/store/accessStore";
import { buildLoginRedirectPath } from "@/utils/safeRedirect";

const isApiEnvelope = <T>(value: unknown): value is IApiRspEnvelop<T> => {
  if (!value || typeof value !== "object") {
    return false;
  }
  const code = Reflect.get(value, "code");
  const message = Reflect.get(value, "msg");
  const errorMessage = Reflect.get(value, "errMsg");
  return typeof code === "number" &&
    (typeof message === "string" || message === null) &&
    (typeof errorMessage === "string" || errorMessage === null) &&
    typeof Reflect.get(value, "tid") === "string" &&
    Reflect.has(value, "data");
};

const traceID = (response: IApiRspEnvelop<unknown>) => {
  if (!response.data || typeof response.data !== "object") {
    return response.tid;
  }
  const requestID = Reflect.get(response.data, "requestId");
  return response.tid || (typeof requestID === "string" ? requestID : undefined);
};

const showTransportError = (error: unknown) => {
  const message = error instanceof Error && error.message ? error.message : "无法连接到服务";
  notifyError("请求失败", message);
};

type FetchJsonArgs = {
  url?: string;
  method: string;
  params?: object;
  data?: unknown;
  retryCount?: number;
  authHeader?: Record<string, string>;
  ignoreGlobalError?: boolean;
  signal?: AbortSignal;
};

export async function FetchJson<T>(args: FetchJsonArgs): Promise<IApiRspEnvelop<T>> {
  return performRequest<T>(args, 0);
}

// attempt 0 is the normal request; attempt 1 is the single self-heal retry
// FetchJson makes after a successful token refresh (see the 401 branch
// below). args.url is the caller's original, unmodified value on every call
// so re-deriving the request below never double-appends a query string.
async function performRequest<T>({
  url: originalUrl, method, params, data, retryCount = 0, authHeader = undefined, ignoreGlobalError = false, signal
}: FetchJsonArgs, attempt: number): Promise<IApiRspEnvelop<T>> {
  let url = originalUrl;
  const shouldNormalizeResponse = isFirstPartyApiUrl(url);
  if (!url?.startsWith(API_BASE_URL)) {
    url = `${API_BASE_URL}${url}`;
  }
  const opts: RequestInit = {
    method,
    signal,
    headers: {
      "Content-Type": "application/json",
    },
  };

  // process params
  if (params) {
    var queryString = qs.stringify(params);
    url += "?" + queryString;
  }

  // process data
  switch (method) {
    case "POST":
    case "PUT":
      if (!data) {
        data = {};
      }
      opts.body = JSON.stringify(data);
      break;
    default:
      if (data) {
        var queryString = qs.stringify(data);
        url += "?" + queryString;
      }
      break;
  }

  let rawResponse: unknown;
  try {
    rawResponse = await Fetch(`${PREFIX}${url}`, opts, retryCount, authHeader);
  } catch (error) {
    if (!ignoreGlobalError) {
      showTransportError(error);
    }
    throw error;
  }
  if (shouldNormalizeResponse) {
    rawResponse = normalizeApiResponse(rawResponse);
  }
  if (!isApiEnvelope<T>(rawResponse)) {
    throw new Error("invalid API response envelope");
  }
  const resp = rawResponse;
  const { code, msg, errMsg } = resp;
  if (!ignoreGlobalError) {
    if (code !== 200) {
      console.log(`Json API: ${code}:${errMsg}`);
      if (code === 401) {
        // 先试一次"自愈"：只要我们当前认为自己是登录状态（hasValidSession），
        // 就说明这个 401 大概率只是 access token 恰好先过期了一步，HttpOnly
        // 的 yaginx-refresh cookie 很可能还有效——用它续签一次再重试原请求，
        // 用户完全无感。这同时天然跳过了登录页密码错误的 401（此时
        // isLoginSuccess 还是 false），不会误触发续签。
        if (attempt === 0 && useAccessStore.getState().hasValidSession()) {
          const refreshed = await useAccessStore.getState().ensureFreshToken();
          if (refreshed) {
            return performRequest<T>({ url: originalUrl, method, params, data, retryCount, authHeader, ignoreGlobalError, signal }, attempt + 1);
          }
        }
        console.log(`Json API access denied`);
        notifyError(`未授权: ${code}`, msg ?? undefined);
        // 续签也失败了：会话确实失效，清掉本地 token 并强制退回登录页，
        // 避免用户停留在一个已经失效、但界面上看起来仍"已登录"的页面上。
        useAccessStore.getState().emptyToken();
        if (typeof window !== "undefined" && window.location.pathname !== "/login") {
          const redirect = buildLoginRedirectPath(window.location.pathname, window.location.search);
          window.location.assign(`/login?redirect=${encodeURIComponent(redirect)}`);
        }
        return resp;
      }
      // 远程调用ApiStatusCode错误
      console.log({ code, errMsg });
      const id = traceID(resp);
      const detail = errMsg || msg || "服务未能完成请求";
      notifyError(`业务调用失败: ${code}`, id ? `${detail}（追踪号: ${id}）` : detail);

      throw { code, reason: errMsg, traceId: id };
    }
  }
  return resp;
}
