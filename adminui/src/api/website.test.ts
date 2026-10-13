import { beforeEach, describe, expect, it, vi } from "vitest";
import { FetchJson } from "@/requests/FetchJson";
import { requestHistoryGet, websiteResponseID, websiteSearch } from "./website";

vi.mock("@/requests/FetchJson", () => ({
  FetchJson: vi.fn(),
}));

describe("requestHistoryGet", () => {
  beforeEach(() => {
    vi.mocked(FetchJson).mockReset();
  });

  it("sends Snowflake IDs as their original decimal strings", async () => {
    const id = "79626516022960137";
    vi.mocked(FetchJson).mockResolvedValue({} as never);

    await requestHistoryGet({ id });

    expect(FetchJson).toHaveBeenCalledWith({
      url: "/api/request_history/get",
      method: "GET",
      params: { id },
    });
  });
});

describe("server pagination requests", () => {
  beforeEach(() => {
    vi.mocked(FetchJson).mockReset();
  });

  it("forwards only canonical page fields and business filters", async () => {
    vi.mocked(FetchJson).mockResolvedValue({ code: 200, data: { records: [], paging: { pageIndex: 1, pageSize: 50, total: 0 } } } as never);

    await websiteSearch({ domain: " configurable.example.invalid ", pageIndex: 2, pageSize: 100 });

    expect(FetchJson).toHaveBeenCalledWith({
      url: "/api/website/search",
      method: "POST",
      data: { domain: " configurable.example.invalid ", pageIndex: 2, pageSize: 100 },
    });
  });
});

describe("website response IDs", () => {
  it("accepts only successful responses with a positive string ID", () => {
    expect(websiteResponseID({ code: 200, data: { id: "101" } })).toBe("101");
    expect(websiteResponseID({ code: 200, data: { id: "0" } })).toBeNull();
    expect(websiteResponseID({ code: 500, data: { id: "101" } })).toBeNull();
    expect(websiteResponseID({ code: 200, data: null })).toBeNull();
  });
});
