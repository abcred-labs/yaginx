import { describe, expect, it } from "vitest";
import { analyticsFailureSummary } from "./analyticsDiagnostics";

describe("analyticsFailureSummary", () => {
  it("merges failed requests and deduplicates their trace IDs", () => {
    expect(analyticsFailureSummary([
      { code: 500, tid: "trace-a" },
      { code: 500, data: { requestId: "trace-a" } },
      { code: 500, tid: "trace-b" },
    ])).toBe("指标加载失败（3 项请求）。追踪号：trace-a、trace-b");
  });

  it("returns undefined when every request succeeds", () => {
    expect(analyticsFailureSummary([{ code: 200 }, { code: 200 }, { code: 200 }])).toBeUndefined();
  });
});
