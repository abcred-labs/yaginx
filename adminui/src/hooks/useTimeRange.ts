import dayjs, { type Dayjs } from "dayjs";
import { useMemo, useState } from "react";

export const DEFAULT_TIME_PRESETS = [{ label: "1 小时", value: 3600 }, { label: "24 小时", value: 86400 }, { label: "7 天", value: 604800 }];

export const useTimeRange = (initialSeconds: number) => {
  const [seconds, setSeconds] = useState(initialSeconds);
  const [custom, setCustom] = useState<[Dayjs | null, Dayjs | null] | null>(null);
  const range = useMemo(() => {
    const to = dayjs().unix();
    if (custom?.[0] && custom[1]) return { from: custom[0].unix(), to: custom[1].unix() };
    return { from: to - seconds, to };
  }, [seconds, custom]);
  return { seconds, setSeconds, custom, setCustom, range };
};
