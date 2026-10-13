import { describe, expect, it } from "vitest";
import { navigationItems } from "./HostTrafficPage";

describe("traffic analysis navigation", () => {
  it("keeps all detailed views under one canonical workspace", () => {
    expect(navigationItems).toHaveLength(4);
    expect(navigationItems.every((item) => item.path.startsWith("/monitor/traffic/"))).toBe(true);
    expect(new Set(navigationItems.map((item) => item.path)).size).toBe(4);
  });
});
