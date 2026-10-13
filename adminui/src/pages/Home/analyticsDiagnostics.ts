type AnalyticsResponse = {
  code: number;
  tid?: string;
  data?: unknown;
};

const requestID = (response: AnalyticsResponse) => {
  if (response.data && typeof response.data === "object" && "requestId" in response.data) {
    const value = (response.data as { requestId?: unknown }).requestId;
    return typeof value === "string" ? value : undefined;
  }
  return undefined;
};

export const analyticsFailureSummary = (responses: AnalyticsResponse[]) => {
  const failures = responses.filter((response) => response.code !== 200);
  const traceIDs = [...new Set(failures.map((response) => response.tid || requestID(response)).filter((value): value is string => Boolean(value)))];
  if (failures.length === 0) {
    return undefined;
  }
  const traceText = traceIDs.length ? `追踪号：${traceIDs.join("、")}` : "未返回追踪号";
  return `指标加载失败（${failures.length} 项请求）。${traceText}`;
};
