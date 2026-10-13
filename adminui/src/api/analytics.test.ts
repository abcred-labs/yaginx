import { describe, expect, it } from "vitest";
import { defaultResourceViewWindowSeconds } from "../pages/ResourceViews/resourceViewDefaults";
import { FetchJson } from "@/requests/FetchJson";
import { getResourceCatalog } from "./analytics";
import { beforeEach, vi } from "vitest";

vi.mock("@/requests/FetchJson", () => ({ FetchJson: vi.fn() }));

describe("resource view defaults", () => {
  beforeEach(() => vi.mocked(FetchJson).mockReset());

  it("opens resource details on a 24-hour observation window", () => {
    expect(defaultResourceViewWindowSeconds).toBe(24 * 60 * 60);
  });

  it("rejects numeric associated website IDs from legacy responses", async () => {
    vi.mocked(FetchJson).mockResolvedValue({ code: 200, data: { records: [{ host: "example.test", websites: [{ id: 2066836992053874700, name: "site" }] }] } } as never);
    await expect(getResourceCatalog({})).rejects.toThrow("接口 ID 格式不兼容");
  });
});
