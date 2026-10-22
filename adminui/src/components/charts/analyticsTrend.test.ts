import type { IAnalyticsPoint } from "@/api/analytics";
import { describe, expect, it } from "vitest";
import { buildAnalyticsTrendOption, forecastTerminalPartialPoint } from "./analyticsTrend";

const palette = {
  flow: "#2575ff",
  alert: "#d54b45",
  grid: "#dbe4ef",
  neutral: "#66748a",
  text: "#526176"
};

const point = (
  bucketStartAt: number,
  requestsPerSecond: number,
  errorRequests: number,
  isPartial = false
): IAnalyticsPoint => ({
  bucketStartAt,
  isPartial,
  requests: 0,
  requestsPerSecond,
  inboundBytes: 0,
  outboundBytes: 0,
  inboundBytesPerSecond: 0,
  outboundBytesPerSecond: 0,
  errorRequests,
  errorRate: 0,
  p50Ms: null,
  p95Ms: null,
  p99Ms: null,
  avgDurationMs: 0,
  maxDurationMs: 0,
  latencyCoverageRate: 0,
  statusBreakdown: [],
  durationBuckets: []
});

describe("terminal analytics forecast", () => {
  it("uses a weighted average across the latest five completed buckets", () => {
    const forecast = forecastTerminalPartialPoint([
      point(1, 1, 1),
      point(2, 2, 2),
      point(3, 3, 3),
      point(4, 4, 4),
      point(5, 5, 5),
      point(6, 0, 0, true)
    ]);

    expect(forecast).toEqual({ requestsPerSecond: 3.6667, errorRequests: 4 });
  });

  it("uses all available completed buckets when fewer than five exist", () => {
    const forecast = forecastTerminalPartialPoint([
      point(1, 2, 1),
      point(2, 6, 3),
      point(3, 0, 0, true)
    ]);

    expect(forecast).toEqual({ requestsPerSecond: 4.6667, errorRequests: 2 });
  });

  it("does not forecast an already completed terminal bucket", () => {
    expect(forecastTerminalPartialPoint([point(1, 4, 1), point(2, 5, 1)])).toBeUndefined();
  });

  it("does not invent a value without a completed historical bucket", () => {
    expect(forecastTerminalPartialPoint([point(1, 1, 1, true)])).toBeUndefined();
  });

  it("splits the terminal partial bucket into dashed forecast series", () => {
    const option = buildAnalyticsTrendOption([
      point(1, 4, 1),
      point(2, 6, 2),
      point(3, 1, 0, true)
    ], palette);
    const series = option.series as Array<Record<string, unknown>>;

    expect(series[0].data).toEqual([4, 6, null]);
    expect(series[1].data).toEqual([1, 2, null]);
    expect(series[2]).toMatchObject({
      name: "Requests/s 预估",
      data: [null, 6, 5.3333],
      lineStyle: { type: "dashed" }
    });
    expect(series[3]).toMatchObject({
      name: "Errors 预估",
      data: [null, null, 2],
      itemStyle: { borderType: "dashed", opacity: 0.6 }
    });
    expect(option.legend).toMatchObject({
      data: ["Requests/s", "Errors", "Requests/s 预估", "Errors 预估"]
    });
  });

  it("formats throughput tooltip values with at most four decimal places", () => {
    const option = buildAnalyticsTrendOption([point(1, 1.234567, 2)], palette);
    const tooltip = option.tooltip as { formatter: (params: unknown) => string };

    expect(tooltip.formatter([{
      dataIndex: 0,
      marker: "",
      seriesName: "Requests/s",
      value: 1.234567
    }])).toContain("Requests/s: 1.2346");
  });
});
