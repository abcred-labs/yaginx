import { describe, expect, it } from "vitest";
import { formatAxisNumber, formatBytes, formatNumber, formatPercent } from "./formatters";

describe("number formatters", () => {
  it("rounds decimal values to at most four places without padding", () => {
    expect(formatNumber(1.23456)).toBe("1.2346");
    expect(formatNumber(1.2)).toBe("1.2");
    expect(formatNumber(1000)).toBe("1,000");
  });

  it("formats ratios as percentages with at most four decimal places", () => {
    expect(formatPercent(0.12345678)).toBe("12.3457%");
    expect(formatPercent(1)).toBe("100%");
  });

  it("formats converted byte units with at most four decimal places", () => {
    expect(formatBytes(1024 * 1.234567)).toBe("1.2346 KB");
    expect(formatBytes(123)).toBe("123 B");
  });

  it("preserves compact axis units while applying the shared decimal precision", () => {
    expect(formatAxisNumber(1234.56789)).toBe("1.2346K");
    expect(formatAxisNumber(1234567.89)).toBe("1.2346M");
    expect(formatAxisNumber(123)).toBe("123");
  });

  it("formats missing and invalid values as zero", () => {
    expect(formatNumber(undefined)).toBe("0");
    expect(formatPercent("invalid")).toBe("0%");
    expect(formatBytes(null)).toBe("0 B");
  });
});
