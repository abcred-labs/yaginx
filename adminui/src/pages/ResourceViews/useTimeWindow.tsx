import { MonDateTimeRange, MonSegmented, type MonDayjsRange } from "@/ui/monParts";
import dayjs from "dayjs";
import React, { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { defaultResourceViewWindowSeconds } from "./resourceViewDefaults";

// 详情类页面统一的时间窗口：预设秒数写入 ?range=，自定义区间写入 ?from=&to=，切换标签或分享链接时保持一致。
export const timeWindowPresets = [{ label: "1 小时", value: 3600 }, { label: "24 小时", value: 86400 }, { label: "7 天", value: 604800 }];

const positiveInt = (value: string | null): number | null => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : null;
};

export interface TimeWindowState {
  window: { from: number; to: number };
  range: number;
  customRange: MonDayjsRange | null;
  setPreset: (seconds: number) => void;
  setCustom: (range: MonDayjsRange | null) => void;
}

export const useTimeWindow = (defaultSeconds = defaultResourceViewWindowSeconds): TimeWindowState => {
  const [searchParams, setSearchParams] = useSearchParams();
  const rangeParam = positiveInt(searchParams.get("range"));
  const range = timeWindowPresets.some((preset) => preset.value === rangeParam) ? rangeParam! : defaultSeconds;
  const from = positiveInt(searchParams.get("from"));
  const to = positiveInt(searchParams.get("to"));
  const custom = from && to && to > from ? { from, to } : null;

  const update = useCallback((mutate: (next: URLSearchParams) => void) => {
    setSearchParams((current) => { const next = new URLSearchParams(current); mutate(next); return next; }, { replace: true });
  }, [setSearchParams]);

  const setPreset = useCallback((seconds: number) => update((next) => { next.set("range", String(seconds)); next.delete("from"); next.delete("to"); }), [update]);
  const setCustom = useCallback((value: MonDayjsRange | null) => update((next) => {
    if (value?.[0] && value?.[1]) { next.set("from", String(Math.floor(value[0].valueOf() / 1000))); next.set("to", String(Math.floor(value[1].valueOf() / 1000))); next.delete("range"); }
    else { next.delete("from"); next.delete("to"); }
  }), [update]);

  const window = useMemo(() => {
    if (custom) return custom;
    const end = Math.floor(Date.now() / 1000);
    return { from: end - range, to: end };
  }, [range, custom?.from, custom?.to]);
  const customRange = useMemo<MonDayjsRange | null>(() => custom ? [dayjs.unix(custom.from), dayjs.unix(custom.to)] : null, [custom?.from, custom?.to]);
  return { window, range, customRange, setPreset, setCustom };
};

export const TimeWindowControl: React.FC<{ state: TimeWindowState }> = ({ state }) => <>
  <MonSegmented options={timeWindowPresets} value={state.range} ariaLabel="统计窗口" onChange={state.setPreset} />
  <MonDateTimeRange value={state.customRange} onChange={state.setCustom} className="w-[320px]" />
</>;
