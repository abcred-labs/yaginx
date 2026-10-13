import dayjs, { Dayjs } from "dayjs";

export interface HostTrafficFilters {
  hostName: string;
  from: number;
  to: number;
}

export const hostTrafficRangePresets = [
  { label: "近 15 分钟", value: "15m", minutes: 15 },
  { label: "近 30 分钟", value: "30m", minutes: 30 },
  { label: "近 1 小时", value: "1h", minutes: 60 },
  { label: "近 24 小时", value: "24h", minutes: 24 * 60 },
  { label: "近 7 天", value: "7d", minutes: 7 * 24 * 60 }
] as const;

const normalizeTimestamp = (value: string | null): number | null => {
  const numericValue = Number(value);
  return Number.isFinite(numericValue) && numericValue > 0 ? Math.floor(numericValue) : null;
};

export const buildHostTrafficRange = (minutes: number, now: Dayjs = dayjs()) => {
  const periodEnd = now.startOf("minute");
  return {
    from: periodEnd.subtract(minutes - 1, "minute").unix(),
    to: periodEnd.unix()
  };
};

export const defaultHostTrafficFilters = (now: Dayjs = dayjs()): HostTrafficFilters => ({
  hostName: "",
  ...buildHostTrafficRange(60, now)
});

export const filtersFromSearchParams = (searchParams: URLSearchParams, now: Dayjs = dayjs()): HostTrafficFilters => {
  const fallback = defaultHostTrafficFilters(now);
  const from = normalizeTimestamp(searchParams.get("from"));
  const to = normalizeTimestamp(searchParams.get("to"));

  return {
    hostName: searchParams.get("host")?.trim() ?? "",
    from: from && to && to > from ? from : fallback.from,
    to: from && to && to > from ? to : fallback.to
  };
};

export const filtersToSearchParams = (filters: HostTrafficFilters): URLSearchParams => {
  const searchParams = new URLSearchParams();
  if (filters.hostName.trim()) {
    searchParams.set("host", filters.hostName.trim());
  }
  searchParams.set("from", String(filters.from));
  searchParams.set("to", String(filters.to));
  return searchParams;
};

export const rangeFromPicker = (range: [Dayjs | null, Dayjs | null]): Pick<HostTrafficFilters, "from" | "to"> | null => {
  const [start, end] = range;
  if (!start || !end || !end.isAfter(start)) {
    return null;
  }
  return {
    from: start.startOf("minute").unix(),
    to: end.startOf("minute").unix()
  };
};

export const rangePresetValue = (filters: HostTrafficFilters): string => {
  const duration = filters.to - filters.from;
  return hostTrafficRangePresets.find((item) => (item.minutes - 1) * 60 === duration)?.value ?? "custom";
};

export const formatTrafficPeriod = (period: number | string | null | undefined, format = "MM-DD HH:mm"): string => {
  const numericPeriod = Number(period);
  return Number.isFinite(numericPeriod) && numericPeriod > 0 ? dayjs.unix(numericPeriod).format(format) : "—";
};

export const trafficGrainLabel = (grain: "minute" | "hour" | "day" | undefined): string => ({
  minute: "分钟",
  hour: "小时",
  day: "天"
}[grain ?? "minute"] ?? "分钟");
