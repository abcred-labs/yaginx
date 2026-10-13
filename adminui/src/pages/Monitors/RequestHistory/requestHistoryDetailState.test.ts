import { describe, expect, it } from "vitest";
import { requestHistoryDetailViewState } from "./requestHistoryDetailState";

describe("requestHistoryDetailViewState", () => {
  it("shows a missing state only after a successful null detail response", () => {
    expect(requestHistoryDetailViewState(true, false, false)).toBe("loading");
    expect(requestHistoryDetailViewState(false, true, false)).toBe("missing");
  });

  it("keeps detail and failed-request blank states distinct from a missing record", () => {
    expect(requestHistoryDetailViewState(false, false, true)).toBe("detail");
    expect(requestHistoryDetailViewState(false, false, false)).toBe("idle");
  });
});
