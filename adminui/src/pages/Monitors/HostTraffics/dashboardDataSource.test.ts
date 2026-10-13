import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const source = (name: string) => readFileSync(new URL(`./${name}`, import.meta.url), "utf8");

describe("Host traffic dashboard data source", () => {
  it("loads request trend through one abortable dashboard request", () => {
    const requestTrend = source("RequestTrend.tsx");

    expect(requestTrend).toContain("getAnalyticsOverview");
    expect(requestTrend).toContain('["overview", "timeseries"]');
    expect(requestTrend).toContain("AbortController");
    expect(requestTrend).toContain("signal: controller.signal");
    expect(requestTrend).not.toContain("Promise.all");
    expect(requestTrend).not.toContain("getObservabilityOverview");
    expect(requestTrend).not.toContain("getAnalyticsTimeseries");
  });

  it("loads bandwidth analysis through one abortable dashboard request", () => {
    const bandwidth = source("BandwidthAnalysis.tsx");

    expect(bandwidth).toContain("getAnalyticsOverview");
    expect(bandwidth).toContain('["overview", "domains", "timeseries"]');
    expect(bandwidth).toContain("AbortController");
    expect(bandwidth).toContain("signal: controller.signal");
    expect(bandwidth).not.toContain("Promise.all");
    expect(bandwidth).not.toContain("getObservabilityOverview");
    expect(bandwidth).not.toContain("getAnalyticsDomains");
    expect(bandwidth).not.toContain("getAnalyticsTimeseries");
  });
});
