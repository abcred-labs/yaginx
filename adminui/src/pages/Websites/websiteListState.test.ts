import { describe, expect, it } from "vitest";
import { buildWebsiteSearchRequest, websiteBatchDeleteResultText, websiteListOperationPath } from "./websiteListState";

describe("website list state", () => {
  it("trims and omits empty search filters", () => {
    expect(buildWebsiteSearchRequest({
      domain: "  api.example.test  ",
      upstream: "   ",
      hasCertificate: false,
    })).toEqual({
      domain: "api.example.test",
      hasCertificate: false,
    });
  });

  it("reports mixed batch deletion results", () => {
    expect(websiteBatchDeleteResultText({ deletedIds: ["1", "2"], notFoundIds: ["3"] })).toBe("已删除 2 个站点，1 个站点不存在。");
  });

  it("uses the current workspace and routes addresses", () => {
    expect(websiteListOperationPath("edit", "42")).toBe("../42");
    expect(websiteListOperationPath("childRules", "42")).toBe("../42/routes");
    expect(websiteListOperationPath("analytics", "2066836992053874700")).toBe("../2066836992053874700/analytics");
  });
});
