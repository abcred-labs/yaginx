import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("Home traffic data source", () => {
  it("uses analytics and does not restore legacy report API calls", () => {
    const source = readFileSync(new URL("./index.tsx", import.meta.url), "utf8");

    expect(source).toContain("AnalyticsDashboard");
    expect(source).not.toContain("resourceReport");
    expect(source).not.toContain("getDashboardSummary");
    expect(source).not.toContain("getAllReportData");
    expect(source).not.toContain("getTypeReportData");
  });
});
