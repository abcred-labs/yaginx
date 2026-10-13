import { describe, expect, it, vi } from "vitest";
import { FetchInternal } from "./FetchInternal";

describe("FetchInternal", () => {
  it("returns first-party JSON error envelopes instead of throwing on HTTP 500", async () => {
    const response = { code: 500, msg: "调用异常", tid: "trace-123", data: { requestId: "trace-123" } };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(response), {
      status: 500,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(FetchInternal("/api/analytics/overview")).resolves.toEqual(response);
    vi.unstubAllGlobals();
  });
});
