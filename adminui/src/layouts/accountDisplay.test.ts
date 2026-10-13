import { describe, expect, it } from "vitest";
import { formatSessionRemaining, getAccountDisplay, getSessionRemainingSeconds } from "./accountDisplay";

describe("account display helpers", () => {
  it("splits an email address into its local name and domain", () => {
    expect(getAccountDisplay("operator@example.com")).toEqual({
      email: "operator@example.com",
      localName: "operator",
      domainName: "example.com",
      initial: "O",
    });
  });

  it("keeps malformed and empty email values readable", () => {
    expect(getAccountDisplay("operator")).toMatchObject({
      localName: "operator",
      domainName: "",
      initial: "O",
    });
    expect(getAccountDisplay("@example.com")).toMatchObject({
      localName: "?",
      domainName: "example.com",
      initial: "?",
    });
    expect(getAccountDisplay()).toMatchObject({
      localName: "?",
      domainName: "",
      initial: "?",
    });
  });

  it("clamps and formats the session refresh countdown", () => {
    expect(getSessionRemainingSeconds(101, 100_000)).toBe(1);
    expect(getSessionRemainingSeconds(99, 100_000)).toBe(0);
    expect(formatSessionRemaining(125)).toBe("2m 5s");
    expect(formatSessionRemaining(-1)).toBe("0s");
  });
});
