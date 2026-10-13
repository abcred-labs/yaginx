// 登录成功 / 跳转登录页时使用的回跳路径，必须限制在站内相对路径，
// 否则 `?redirect=` 就成了一个开放重定向（open redirect）入口。
export const isSafeRedirect = (path?: string | null): path is string => {
  if (!path) return false;
  if (!path.startsWith("/")) return false;
  // "//evil.com" 或 "/\evil.com" 会被浏览器当成协议相对 URL 处理，拒绝。
  if (path.startsWith("//") || path.startsWith("/\\")) return false;
  return true;
};

export const buildLoginRedirectPath = (pathname: string, search: string): string => {
  const target = `${pathname}${search}`;
  return isSafeRedirect(target) ? target : "/";
};
