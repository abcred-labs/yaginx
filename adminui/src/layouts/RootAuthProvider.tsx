import React, { createContext, useContext, useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { DEFAULT_AUTH_INFO, IAuthInfo, useAccessStore } from "@/store/accessStore";
import { logout as apiLogout } from "@/api/account";
import workerScript from "@/woker";
export interface IAuthContext {
  authInfo: IAuthInfo,
  isAuthorized: boolean,
  // 第一次"这个浏览器到底算不算已登录"的判定（挂载时的静默刷新）是否已经跑完。
  // 在它变 true 之前，消费方（如 UserLayout）应该渲染 Loading 而不是直接按
  // isAuthorized=false 把人踢到登录页——否则 token 移出 localStorage 之后，
  // 每次刷新页面都会先闪一下登录页，即使 refresh cookie 其实还有效。
  authReady: boolean,
  login: (data: IAuthInfo) => void,
  logout: () => void
}
const DEFAULT_AUTH_CONTEXT: IAuthContext = {
  authInfo: DEFAULT_AUTH_INFO,
  isAuthorized: false,
  authReady: false,
  login: (data: IAuthInfo) => { },
  logout: () => { }
}
// https://blog.logrocket.com/complete-guide-authentication-with-react-router-v6/
const AuthContext = createContext<IAuthContext>(DEFAULT_AUTH_CONTEXT);

// 负责提供Auth信息
export const RootAuthProvider = ({ children }: any) => {
  // useAccessStore() subscribes to the whole store, so this component
  // re-renders on every field change. authInfo/isAuthorized are derived
  // fresh on each render instead of mirrored into local useState: a mirror
  // keyed off "did the token change" silently stops updating once the token
  // is no longer persisted, because a failed refresh goes ""->"" (emptyToken
  // on an already-empty token) — no change for that effect to catch, so
  // isAuthorized would get stuck at its stale optimistic initial value while
  // authReady correctly flips to true. Deriving directly avoids that class of
  // bug entirely.
  const accessStore = useAccessStore();
  const navigate = useNavigate();

  useEffect(() => {
    // 启动时做一次真正的校验/续签检查。
    accessStore.checkAuthInfo();

    let worker = new Worker(workerScript);
    worker.onmessage = (m: any) => {
      accessStore.checkAuthInfo();
    };

    return () => {
      // 记得在组件销毁时清除worker
      worker.terminate();
    }
  }, [])

  useEffect(() => {
    // 标签页从后台切回来 / 电脑休眠唤醒后，不等 worker 下一次 10s 轮询，
    // 立刻补一次续签检查——这段时间里定时器可能根本没机会跑，access token
    // 就这样在没人续签的情况下悄悄过期了。
    const onVisible = () => {
      if (document.visibilityState === "visible") {
        accessStore.checkAuthInfo();
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", onVisible);
    };
  }, [])

  const login = async (data: IAuthInfo) => {
    accessStore.updateToken(data);
  };

  const logout = () => {
    // 尽力而为：清掉服务端那条 refresh session + 两个 cookie；本地状态不等
    // 它返回就先清，避免网络慢的时候用户点了退出却没反应。
    apiLogout().catch(() => { });
    accessStore.emptyToken();
    navigate("/login", { replace: true });
  };

  const authInfo = accessStore.authInfo();
  const isAuthorized = accessStore.hasValidSession();
  const value: IAuthContext = useMemo(
    () => ({ authInfo, isAuthorized, authReady: accessStore.authReady, login, logout }),
    [accessStore.token, accessStore.tokenExpireTime, isAuthorized, accessStore.authReady],
  );
  return (<AuthContext.Provider value={value}>{children}</AuthContext.Provider>);
};
export const useAuth = () => useContext(AuthContext);
