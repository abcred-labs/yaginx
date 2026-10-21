import { describe, expect, it } from "vitest";
import { buildServerPageRequest } from "@/ui/serverPageRequest";

describe("buildServerPageRequest", () => {
  it("removes Ant Design pagination fields from the API request", () => {
    expect(buildServerPageRequest(
      { keyword: "example", current: 9, pageSizeOptions: ["50", "100"] },
      { includeRemoved: false },
      { pageIndex: 2, pageSize: 100 },
    )).toEqual({ keyword: "example", includeRemoved: false, pageIndex: 2, pageSize: 100 });
  });
});
