import type { IAnalyticsContributionPoint, IAnalyticsHostRanking, IAnalyticsPoint } from "@/api/analytics";
import { chartPalette } from "@/appearance";
import { formatAxisNumber, formatBytes, formatNumber, formatPercent, toNumber } from "@/utils/formatters";
import type { EChartsOption } from "echarts";
import { formatTrafficPeriod } from "./shared";

type Palette = ReturnType<typeof chartPalette>;
type Grain = "minute" | "hour" | "day";

const timeFormatFor = (grain: Grain) => grain === "day" ? "MM-DD" : grain === "hour" ? "MM-DD HH:00" : "MM-DD HH:mm";

const sharedXAxis = (labels: string[], colors: Palette) => ({
  type: "category" as const,
  boundaryGap: false,
  data: labels,
  axisLine: { lineStyle: { color: colors.grid } },
  axisLabel: { color: colors.neutral, hideOverlap: true }
});

const sharedYAxis = (colors: Palette, formatter: (value: number) => string) => ({
  type: "value" as const,
  axisLabel: { color: colors.neutral, formatter },
  axisLine: { show: false },
  splitLine: { lineStyle: { color: colors.grid, type: "dashed" as const } }
});

export const buildRequestTrendOption = (points: IAnalyticsPoint[], grain: Grain, colors: Palette): EChartsOption => {
  const labels = points.map((point) => formatTrafficPeriod(point.bucketStartAt, timeFormatFor(grain)));
  return {
    color: [colors.flow],
    grid: { left: 58, right: 20, top: 24, bottom: 34 },
    tooltip: {
      trigger: "axis",
      formatter: (params: any) => {
        const item = params?.[0];
        const point = points[item?.dataIndex ?? 0];
        if (!point) return "";
        return `${formatTrafficPeriod(point.bucketStartAt, "YYYY-MM-DD HH:mm")}<br/>${item.marker}请求量：${formatNumber(point.requests)} 次${point.isPartial ? "<br/>当前桶仍在实时汇总" : ""}`;
      }
    },
    xAxis: sharedXAxis(labels, colors),
    yAxis: sharedYAxis(colors, formatAxisNumber),
    series: [{
      name: "请求量",
      type: "line",
      smooth: true,
      symbol: "circle",
      symbolSize: (value: number, params: { dataIndex: number }) => points[params.dataIndex]?.isPartial ? 8 : value > 0 ? 4 : 0,
      lineStyle: { width: 3 },
      areaStyle: { color: colors.flow, opacity: 0.14 },
      data: points.map((point) => ({
        value: point.requests,
        itemStyle: point.isPartial ? { borderWidth: 2, borderColor: colors.warning, color: "#ffffff" } : undefined
      }))
    }]
  };
};

export const buildRequestContributionOption = (timeline: IAnalyticsContributionPoint[], grain: Grain, colors: Palette): EChartsOption => {
  const periods = Array.from(new Set(timeline.map((item) => Number(item.period))));
  const hostNames = Array.from(new Set(timeline.map((item) => item.hostName)));
  const colorScale = [colors.flow, colors.signal, colors.warning, colors.accent, colors.alert, colors.neutral];

  return {
    color: colorScale,
    grid: { left: 58, right: 20, top: 46, bottom: 34 },
    tooltip: {
      trigger: "axis",
      axisPointer: { type: "shadow" },
      valueFormatter: (value) => `${formatNumber(value as number)} 次`
    },
    legend: {
      top: 0,
      type: "scroll",
      textStyle: { color: colors.text }
    },
    xAxis: {
      ...sharedXAxis(periods.map((period) => formatTrafficPeriod(period, timeFormatFor(grain))), colors),
      boundaryGap: true
    },
    yAxis: sharedYAxis(colors, formatAxisNumber),
    series: hostNames.map((hostName) => ({
      name: hostName,
      type: "bar",
      stack: "requests",
      emphasis: { focus: "series" },
      data: periods.map((period) => toNumber(timeline.find((item) => Number(item.period) === period && item.hostName === hostName)?.requestCounts))
    }))
  };
};

export const buildRequestRankOption = (rankings: IAnalyticsHostRanking[], colors: Palette): EChartsOption => ({
  color: [colors.flow],
  grid: { left: 148, right: 58, top: 14, bottom: 18 },
  tooltip: {
    trigger: "axis",
    axisPointer: { type: "shadow" },
    formatter: (params: any) => {
      const item = rankings[params[0]?.dataIndex ?? 0];
      return `${item?.hostName ?? ""}<br/>${formatNumber(item?.requestCounts)} 次 · ${formatPercent(item?.share)}`;
    }
  },
  xAxis: sharedYAxis(colors, formatAxisNumber),
  yAxis: {
    type: "category",
    inverse: true,
    data: rankings.map((item) => item.hostName),
    axisLine: { lineStyle: { color: colors.grid } },
    axisLabel: {
      color: colors.neutral,
      width: 130,
      overflow: "truncate"
    }
  },
  series: [{
    name: "请求量",
    type: "bar",
    barMaxWidth: 22,
    label: {
      show: true,
      position: "right",
      color: colors.neutral,
      formatter: (params: any) => formatPercent(rankings[params.dataIndex]?.share)
    },
    data: rankings.map((item) => toNumber(item.requestCounts))
  }]
});

export const buildBandwidthTrendOption = (points: IAnalyticsPoint[], grain: Grain, colors: Palette): EChartsOption => {
  const labels = points.map((point) => formatTrafficPeriod(point.bucketStartAt, timeFormatFor(grain)));
  return {
    color: [colors.signal, colors.warning],
    grid: { left: 72, right: 20, top: 46, bottom: 34 },
    tooltip: {
      trigger: "axis",
      valueFormatter: (value) => formatBytes(value as number)
    },
    legend: { top: 0, textStyle: { color: colors.text } },
    xAxis: sharedXAxis(labels, colors),
    yAxis: sharedYAxis(colors, formatBytes),
    series: [
      {
        name: "流入流量",
        type: "line",
        smooth: true,
        symbol: "none",
        areaStyle: { color: colors.signal, opacity: 0.1 },
        data: points.map((point) => toNumber(point.inboundBytes))
      },
      {
        name: "流出流量",
        type: "line",
        smooth: true,
        symbol: "none",
        areaStyle: { color: colors.warning, opacity: 0.1 },
        data: points.map((point) => toNumber(point.outboundBytes))
      }
    ]
  };
};

export const buildBandwidthRankOption = (rankings: IAnalyticsHostRanking[], colors: Palette): EChartsOption => ({
  color: [colors.signal, colors.warning],
  grid: { left: 148, right: 20, top: 44, bottom: 18 },
  tooltip: {
    trigger: "axis",
    axisPointer: { type: "shadow" },
    formatter: (params: any) => {
      const item = rankings[params[0]?.dataIndex ?? 0];
      return `${item?.hostName ?? ""}<br/>流入 ${formatBytes(item?.inboundBytes)}<br/>流出 ${formatBytes(item?.outboundBytes)}<br/>总计 ${formatBytes(item?.totalBytes)} · ${formatPercent(item?.share)}`;
    }
  },
  legend: { top: 0, textStyle: { color: colors.text } },
  xAxis: sharedYAxis(colors, formatBytes),
  yAxis: {
    type: "category",
    inverse: true,
    data: rankings.map((item) => item.hostName),
    axisLine: { lineStyle: { color: colors.grid } },
    axisLabel: {
      color: colors.neutral,
      width: 130,
      overflow: "truncate"
    }
  },
  series: [
    {
      name: "流入流量",
      type: "bar",
      stack: "traffic",
      barMaxWidth: 24,
      data: rankings.map((item) => toNumber(item.inboundBytes))
    },
    {
      name: "流出流量",
      type: "bar",
      stack: "traffic",
      barMaxWidth: 24,
      data: rankings.map((item) => toNumber(item.outboundBytes))
    }
  ]
});
