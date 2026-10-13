import { createPersistStore } from "./store";
import { refreshSession } from "@/requests/authRefresh";
export enum StoreKey {
  AuthInfo = "auth_info"
}

// Shared by checkAuthInfo (proactive poll) and FetchJson's reactive 401
// handler, so two requests failing around the same time collapse into one
// refresh_token call instead of each rotating the refresh cookie's secret
// out from under the other.
let pendingRefresh: Promise<boolean> | null = null;

export interface IAuthInfo {
  userId: string,
  name: string,
  tokenExpireTime: number,
  expiresIn: number,
  token: string,
  maxValidSeconds: number,
}

export const nowts = () => Math.floor(Date.now() / 1000);
export const DEFAULT_AUTH_INFO: IAuthInfo = {
  tokenExpireTime: Math.floor(Date.now() / 1000),
  expiresIn: 0,
  token: "",
  userId: "",
  name: "",
  maxValidSeconds: 0,
}

const DEFAULT_ACCESS_STATE = {
  isCheckAuthLoopEnable: false,
  isLoginSuccess: false,
  tokenExpireTime: Math.floor(Date.now() / 1000),
  token: "",
  userId: "",
  name: "",
  maxValidSeconds: 0,
  // 不持久化（见下面 partialize）：每次页面加载都从 false 开始，第一次
  // checkAuthInfo 跑完（无论成功失败）才置 true。UserLayout 在它变 true 之前
  // 渲染 Loading，避免"明明还登录着，刷新页面却闪一下登录页"。
  authReady: false,
};

export const useAccessStore = createPersistStore(
  { ...DEFAULT_ACCESS_STATE },

  (set, get) => ({
    emptyToken() {
      set(() => ({
        token: "",
        tokenExpireTime: 0,
        userId: "",
        name: "",
        isLoginSuccess: false,
        isCheckAuthLoopEnable: false
      }));
    },

    updateToken(authInfo: IAuthInfo) {
      const { token, expiresIn, userId, name } = authInfo;
      if (token.length <= 0) return;
      set(() => ({
        token: token?.trim(),
        tokenExpireTime: (Math.floor(Date.now() / 1000) + Number.parseInt(expiresIn.toString()) / 6 * 5), // 提前到5/6的时间开始刷新Token
        maxValidSeconds: Number.parseInt(expiresIn.toString()) / 6 * 5,
        userId: userId,
        name: name,
        isLoginSuccess: true,
        isCheckAuthLoopEnable: true
      }));
    },

    // 纯读取，不触发任何网络请求：供渲染期（如 useState 初始值）安全调用。
    // 真正的续签检查固定走 checkAuthInfo，由 effect/worker 触发。
    hasValidSession() {
      return get().isLoginSuccess;
    },

    isAuthorized() {
      this.checkAuthInfo();
      return get().isLoginSuccess;
    },

    authInfo(): IAuthInfo {
      const obj = get();
      return {
        token: obj.token,
        tokenExpireTime: obj.tokenExpireTime,
        expiresIn: obj.tokenExpireTime - Math.floor(Date.now() / 1000),
        userId: obj.userId,
        name: obj.name,
        maxValidSeconds: obj.maxValidSeconds,
      }
    },

    // 单飞续签：并发多次调用只会真正发出一次 refresh_token 请求，其余调用者
    // 等同一个 Promise。refreshToken() 走的是 HttpOnly yaginx-refresh cookie，
    // 不依赖（内存态的）access token，所以整页刷新后 token 被清空也不影响它。
    ensureFreshToken(): Promise<boolean> {
      if (pendingRefresh) return pendingRefresh;
      pendingRefresh = refreshSession().then((rsp) => {
        if (rsp && rsp.code === 200 && rsp.data) {
          this.updateToken({ ...DEFAULT_AUTH_INFO, ...rsp.data });
          return true;
        }
        this.emptyToken();
        return false;
      }).catch((error) => {
        console.error("refresh token error," + error?.message);
        this.emptyToken();
        return false;
      }).finally(() => {
        pendingRefresh = null;
      });
      return pendingRefresh;
    },

    checkAuthInfo() {
      const markReady = () => {
        if (!get().authReady) set({ authReady: true });
      };

      if (get().isCheckAuthLoopEnable === false) {
        markReady();
        return false;
      }
      const currentTimeInSeconds = Math.floor(Date.now() / 1000);
      if (get().tokenExpireTime > currentTimeInSeconds) {
        markReady();
        return;
      }
      console.debug("start call refresh token from server");
      this.ensureFreshToken().finally(markReady);
    },
  }),
  {
    name: StoreKey.AuthInfo,
    version: 1,
    // createPersistStore's persistOptions type fixes PersistedState = the full
    // state (it doesn't expose zustand's PersistOptions<T, PersistedState>
    // second parameter), so a narrower partialize return doesn't typecheck
    // against it even though zustand supports it at runtime. Scoped `as any`
    // on just this option, not the whole call, so every other property here
    // stays checked.
    partialize: ((state: typeof DEFAULT_ACCESS_STATE) => ({
      isCheckAuthLoopEnable: state.isCheckAuthLoopEnable,
      isLoginSuccess: state.isLoginSuccess,
      userId: state.userId,
      name: state.name,
      // token / tokenExpireTime / maxValidSeconds are intentionally left out:
      // the access token only ever lives in memory now. A reload relies on
      // the HttpOnly refresh cookie (via checkAuthInfo -> refreshToken) to get
      // a new one, not on anything read back from localStorage.
    })) as any,
  },
);
