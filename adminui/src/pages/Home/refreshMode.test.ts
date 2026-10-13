import { describe, expect, it } from "vitest";
import {
  autoRefreshIntervals,
  createAutoRefreshMode,
  createManualRefreshMode,
  refreshButtonText,
  shouldShowDashboardLoading
} from "./refreshMode";

describe("dashboard refresh mode", () => {
  it("defaults to a manual refresh button", () => {
    expect(refreshButtonText(createManualRefreshMode(), false)).toBe("刷新数据");
  });

  it("formats every supported automatic refresh frequency", () => {
    expect(autoRefreshIntervals.map((seconds) => refreshButtonText(createAutoRefreshMode(seconds), false))).toEqual([
      "每 2 秒自动刷新",
      "每 5 秒自动刷新",
      "每 10 秒自动刷新",
      "每 15 秒自动刷新",
      "每 30 秒自动刷新",
      "每 60 秒自动刷新"
    ]);
  });

  it("shows progress only while automatic refresh is in flight", () => {
    expect(refreshButtonText(createAutoRefreshMode(10), true)).toBe("正在自动刷新...");
    expect(shouldShowDashboardLoading("auto")).toBe(false);
    expect(shouldShowDashboardLoading("manual")).toBe(true);
  });

  it("returns to the manual label after the primary action resets the mode", () => {
    expect(refreshButtonText(createManualRefreshMode(), false)).toBe("刷新数据");
  });
});
