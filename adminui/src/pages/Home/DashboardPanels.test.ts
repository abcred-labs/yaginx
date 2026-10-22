import { describe, expect, it } from "vitest";
import { buildTrendWithLatency } from "./DashboardPanels";

describe("dashboard latency trend", () => {
  it("maps latency series to the API's *Ms fields", () => {
    const option = buildTrendWithLatency([{ bucketStartAt: 1, p50Ms: 12, p95Ms: 35, p99Ms: 60 } as never]) as { series: Array<{ name: string; data: unknown[] }> };
    expect(option.series.find((item) => item.name === "P50")?.data).toEqual([12]);
    expect(option.series.find((item) => item.name === "P95")?.data).toEqual([35]);
    expect(option.series.find((item) => item.name === "P99")?.data).toEqual([60]);
  });

  it("keeps the three y axes from overlapping on the right edge", () => {
    const option = buildTrendWithLatency([{ bucketStartAt: 1, p50Ms: 1, p95Ms: 2, p99Ms: 3 } as never]) as unknown as {
      grid: { right: number };
      yAxis: Array<{ position?: string; offset?: number }>;
    };
    expect(option.yAxis).toHaveLength(3);
    const latency = option.yAxis[2];
    expect(latency.position).toBe("right");
    expect(latency.offset).toBeGreaterThan(0);
    // 外移的轴必须在 grid 右侧留出同样的空间。
    expect(option.grid.right).toBeGreaterThanOrEqual(latency.offset ?? 0);
  });
});
