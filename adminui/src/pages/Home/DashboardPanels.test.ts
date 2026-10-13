import { describe, expect, it } from "vitest";
import { buildTrendWithLatency } from "./DashboardPanels";

describe("dashboard latency trend", () => {
  it("maps latency series to the API's *Ms fields", () => {
    const option = buildTrendWithLatency([{ bucketStartAt: 1, p50Ms: 12, p95Ms: 35, p99Ms: 60 } as never]) as { series: Array<{ name: string; data: unknown[] }> };
    expect(option.series.find((item) => item.name === "P50")?.data).toEqual([12]);
    expect(option.series.find((item) => item.name === "P95")?.data).toEqual([35]);
    expect(option.series.find((item) => item.name === "P99")?.data).toEqual([60]);
  });
});
