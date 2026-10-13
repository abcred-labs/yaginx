import { describe, expect, it } from "vitest";
import { menus } from "./menu";

describe("management navigation", () => {
  it("exposes one canonical entry per responsibility", () => {
    const paths = menus.flatMap((section) => section.children?.map((child) => child.path) ?? [section.path]);
    expect(paths).toEqual(expect.arrayContaining([
      "/website",
      "/access-control/ip-blacklist",
      "/monitor/traffic/request-trend",
      "/monitor/requestHistory/list",
      "/resource/hosts",
      "/systems/runtime",
      "/systems/nodes",
      "/systems/acme",
      "/systems/notifications",
      "/systems/integrations",
      "/systems/database",
    ]));
    expect(new Set(paths).size).toBe(paths.length);
    expect(paths.some((path) => path.includes("hostTraffic"))).toBe(false);
  });
});
