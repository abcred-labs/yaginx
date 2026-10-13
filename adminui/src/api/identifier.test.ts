import { describe, expect, it } from "vitest";
import { IdentifierContractError, requireInt64String } from "./identifier";

describe("identifier contract", () => {
  it("accepts large decimal IDs without converting them", () => {
    const id = "2066836992053874700";
    expect(requireInt64String(id)).toBe(id);
  });

  it("rejects numeric IDs before they reach a route", () => {
    expect(() => requireInt64String(2066836992053874700)).toThrow(IdentifierContractError);
  });
});
