import dayjs from "dayjs";
import { describe, expect, it } from "vitest";
import { buildHostTrafficRange, filtersFromSearchParams, filtersToSearchParams, rangePresetValue } from "./shared";

describe("Host traffic filters", () => {
  const now = dayjs("2026-08-03T10:25:00");

  it("defaults to the previous 60 rolling minute buckets", () => {
    const filters = filtersFromSearchParams(new URLSearchParams(), now);

    expect(filters).toEqual({
      hostName: "",
      from: dayjs("2026-08-03T09:26:00").unix(),
      to: dayjs("2026-08-03T10:25:00").unix()
    });
    expect(rangePresetValue(filters)).toBe("1h");
  });

  it("round-trips host and selected range through URL parameters", () => {
    const filters = {
      hostName: "api.example.test",
      ...buildHostTrafficRange(7 * 24 * 60, now)
    };

    expect(filtersFromSearchParams(filtersToSearchParams(filters), now)).toEqual(filters);
    expect(rangePresetValue(filters)).toBe("7d");
  });

  it("keeps a one-hour preset as sixty minute buckets rather than one hour bucket", () => {
    expect(buildHostTrafficRange(60, now)).toEqual({
      from: dayjs("2026-08-03T09:26:00").unix(),
      to: dayjs("2026-08-03T10:25:00").unix()
    });
  });
});
