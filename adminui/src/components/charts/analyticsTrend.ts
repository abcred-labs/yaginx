import type { IAnalyticsPoint } from "@/api/analytics";
import { formatNumber } from "@/utils/formatters";
import type { EChartsOption } from "echarts";

type TrendPalette = {
  flow: string;
  alert: string;
  grid: string;
  neutral: string;
  text: string;
};

export interface TrendForecast {
  requestsPerSecond: number;
  errorRequests: number;
}

const FORECAST_BUCKET_COUNT = 5;
const forecastRequestSeriesName = "Requests/s 预估";
const forecastErrorSeriesName = "Errors 预估";

const roundTo = (value: number, decimalPlaces: number) => {
  const factor = 10 ** decimalPlaces;
  return Math.round(value * factor) / factor;
};

const formatAxisTime = (bucketStartAt: number, includeDate: boolean) => {
  const date = new Date(bucketStartAt * 1000);
  const time = new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false
  }).format(date);

  if (!includeDate) {
    return time;
  }

  const day = new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit"
  }).format(date);
  return `${day} ${time}`;
};

const formatTooltipTime = (bucketStartAt: number) => new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false
}).format(new Date(bucketStartAt * 1000));

const latestCompletedPointIndex = (points: IAnalyticsPoint[]) => {
  for (let index = points.length - 2; index >= 0; index -= 1) {
    if (!points[index].isPartial) {
      return index;
    }
  }
  return -1;
};

// Only replace the terminal partial bucket. Historical points remain API-backed.
export const forecastTerminalPartialPoint = (points: IAnalyticsPoint[]): TrendForecast | undefined => {
  const terminalPoint = points.at(-1);
  if (!terminalPoint?.isPartial) {
    return undefined;
  }

  const history = points
    .slice(0, -1)
    .filter((point) => !point.isPartial)
    .slice(-FORECAST_BUCKET_COUNT)
    .reverse();

  if (!history.length) {
    return undefined;
  }

  const totalWeight = history.reduce((sum, _, index) => sum + history.length - index, 0);
  const weightedValue = (value: (point: IAnalyticsPoint) => number) =>
    history.reduce((sum, point, index) => sum + value(point) * (history.length - index), 0) / totalWeight;

  return {
    requestsPerSecond: roundTo(weightedValue((point) => point.requestsPerSecond), 4),
    errorRequests: Math.max(0, Math.round(weightedValue((point) => point.errorRequests)))
  };
};

export const buildAnalyticsTrendOption = (points: IAnalyticsPoint[], colors: TrendPalette): EChartsOption => {
  const forecast = forecastTerminalPartialPoint(points);
  const terminalIndex = points.length - 1;
  const anchorIndex = forecast ? latestCompletedPointIndex(points) : -1;
  const includeDate = points.length > 1 && points[terminalIndex].bucketStartAt - points[0].bucketStartAt >= 24 * 60 * 60;
  const labels = points.map((point) => formatAxisTime(point.bucketStartAt, includeDate));
  const actualRequests = points.map((point, index) => forecast && index === terminalIndex ? null : point.requestsPerSecond);
  const actualErrors = points.map((point, index) => forecast && index === terminalIndex ? null : point.errorRequests);
  const predictedRequests = points.map((point, index) => {
    if (!forecast) {
      return null;
    }
    if (index === anchorIndex) {
      return point.requestsPerSecond;
    }
    return index === terminalIndex ? forecast.requestsPerSecond : null;
  });
  const predictedErrors = points.map((_, index) => forecast && index === terminalIndex ? forecast.errorRequests : null);

  return {
    color: [colors.flow, colors.alert, colors.flow, colors.alert],
    grid: { left: 58, right: 28, top: 50, bottom: 50, containLabel: true },
    legend: {
      top: 4,
      itemWidth: 24,
      textStyle: { color: colors.text },
      data: forecast ? ["Requests/s", "Errors", forecastRequestSeriesName, forecastErrorSeriesName] : ["Requests/s", "Errors"]
    },
    tooltip: {
      trigger: "axis",
      axisPointer: { type: "line", lineStyle: { color: colors.neutral, type: "dashed" } },
      formatter: (params: unknown) => {
        const entries = Array.isArray(params) ? params as Array<{
          axisValueLabel?: string;
          dataIndex?: number;
          marker?: string;
          seriesName?: string;
          value?: number | null;
        }> : [];
        const displayEntries = entries.filter((item) =>
          item.value != null
          && !(item.seriesName === forecastRequestSeriesName && item.dataIndex !== terminalIndex)
        );
        if (!displayEntries.length) {
          return "";
        }
        const title = formatTooltipTime(points[displayEntries[0].dataIndex ?? 0].bucketStartAt);
        return [
          `<strong>${title}</strong>`,
          ...displayEntries.map((item) => `${item.marker ?? ""}${item.seriesName}: ${formatNumber(item.value)}`)
        ].join("<br />");
      }
    },
    xAxis: {
      type: "category",
      boundaryGap: false,
      data: labels,
      axisLine: { lineStyle: { color: colors.grid } },
      axisTick: { show: false },
      axisLabel: { color: colors.neutral, hideOverlap: true, margin: 14 }
    },
    yAxis: [
      {
        type: "value",
        name: "RPS",
        nameTextStyle: { color: colors.neutral, padding: [0, 0, 0, 4] },
        axisLabel: { color: colors.neutral, formatter: formatNumber },
        splitLine: { lineStyle: { color: colors.grid, type: "dashed" } }
      },
      {
        type: "value",
        name: "Errors",
        nameTextStyle: { color: colors.neutral, padding: [0, 4, 0, 0] },
        axisLabel: { color: colors.neutral },
        splitLine: { show: false }
      }
    ],
    series: [
      {
        name: "Requests/s",
        type: "line",
        smooth: true,
        symbol: "circle",
        symbolSize: 6,
        lineStyle: { width: 3 },
        areaStyle: { color: colors.flow, opacity: 0.11 },
        data: actualRequests,
        z: 3
      },
      {
        name: "Errors",
        type: "bar",
        yAxisIndex: 1,
        barMaxWidth: 18,
        itemStyle: { borderRadius: [6, 6, 0, 0] },
        data: actualErrors,
        z: 1
      },
      {
        name: forecastRequestSeriesName,
        type: "line",
        smooth: false,
        connectNulls: true,
        symbol: "circle",
        symbolSize: 8,
        lineStyle: { width: 3, type: "dashed" },
        itemStyle: { borderWidth: 2, borderColor: colors.flow, color: "#ffffff" },
        data: predictedRequests,
        z: 4
      },
      {
        name: forecastErrorSeriesName,
        type: "bar",
        yAxisIndex: 1,
        barMaxWidth: 18,
        itemStyle: {
          borderRadius: [6, 6, 0, 0],
          borderColor: colors.alert,
          borderType: "dashed",
          borderWidth: 1,
          opacity: 0.6
        },
        data: predictedErrors,
        z: 2
      }
    ]
  };
};
