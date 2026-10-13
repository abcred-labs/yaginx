export const autoRefreshIntervals = [2, 5, 10, 15, 30, 60] as const;

export type AutoRefreshInterval = (typeof autoRefreshIntervals)[number];
export type RefreshRequestKind = "manual" | "auto";
export type RefreshMode =
  | { kind: "manual" }
  | { kind: "auto"; seconds: AutoRefreshInterval };

export const createManualRefreshMode = (): RefreshMode => ({ kind: "manual" });

export const createAutoRefreshMode = (seconds: AutoRefreshInterval): RefreshMode => ({
  kind: "auto",
  seconds
});

export const isAutoRefreshInterval = (value: number): value is AutoRefreshInterval =>
  autoRefreshIntervals.includes(value as AutoRefreshInterval);

export const refreshButtonText = (mode: RefreshMode, isAutoRefreshing: boolean): string => {
  if (mode.kind === "auto" && isAutoRefreshing) {
    return "正在自动刷新...";
  }
  return mode.kind === "auto" ? `每 ${mode.seconds} 秒自动刷新` : "刷新数据";
};

export const shouldShowDashboardLoading = (requestKind: RefreshRequestKind): boolean =>
  requestKind === "manual";
