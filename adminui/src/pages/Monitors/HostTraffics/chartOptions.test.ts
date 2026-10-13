import type { IAnalyticsPoint } from "@/api/analytics";
import { describe, expect, it } from "vitest";
import { buildRequestTrendOption } from "./chartOptions";

const colors = {
  flow: "#1677ff",
  signal: "#13c2c2",
  warning: "#faad14",
  accent: "#722ed1",
  alert: "#ff4d4f",
  neutral: "#8c8c8c",
  text: "#1f1f1f",
  grid: "#d9d9d9"
};

describe("host traffic chart options", () => {
  it("renders request counts and preserves a partial terminal minute", () => {
    const points: IAnalyticsPoint[] = [
      { bucketStartAt: 1722798000, requests: 20, requestsPerSecond: 0, inboundBytes: 0, outboundBytes: 0, inboundBytesPerSecond: 0, outboundBytesPerSecond: 0, errorRequests: 0, errorRate: 0, p50Ms: null, p95Ms: null, p99Ms: null, avgDurationMs: 0, maxDurationMs: 0, latencyCoverageRate: 0, isPartial: false, statusBreakdown: [], durationBuckets: [] },
      { bucketStartAt: 1722798060, requests: 25, requestsPerSecond: 0, inboundBytes: 0, outboundBytes: 0, inboundBytesPerSecond: 0, outboundBytesPerSecond: 0, errorRequests: 0, errorRate: 0, p50Ms: null, p95Ms: null, p99Ms: null, avgDurationMs: 0, maxDurationMs: 0, latencyCoverageRate: 0, isPartial: true, statusBreakdown: [], durationBuckets: [] }
    ];
    const option = buildRequestTrendOption(points, "minute", colors);
    const series = option.series as Array<{ data: Array<{ value: number; itemStyle?: unknown }> }>;
    expect(series[0].data.map((item) => item.value)).toEqual([20, 25]);
    expect(series[0].data[1].itemStyle).toBeDefined();
  });
});
