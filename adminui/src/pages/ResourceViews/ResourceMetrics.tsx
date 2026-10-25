import EChartPanel from "@/components/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/components/charts/analyticsTrend";
import { IAnalyticsOverview, IAnalyticsTimeseries } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import { formatBytes, formatNumber, formatPercent, toNumber } from "@/utils/formatters";
import { Badge, Card, CardContent, Statistic } from "@modra/react";
import React from "react";
import { MonCard } from "@/ui/monParts";

export const metric = (value: number | null | undefined, suffix = "") => value == null ? "不可用" : `${formatNumber(value)}${suffix}`;

// 站点与 Host 详情共用的 KPI 卡片。
export const MetricCards: React.FC<{ overview?: IAnalyticsOverview }> = ({ overview }) => {
  const kpis = overview?.kpis;
  const cards = [
    ["请求总计", formatNumber(kpis?.requests)], ["当前吞吐", metric(kpis?.requestsPerSecond, " req/s")], ["错误率", formatPercent(toNumber(kpis?.errorRate))],
    ["P50 / P95 / P99", `${metric(kpis?.p50Ms, " ms")} / ${metric(kpis?.p95Ms, " ms")} / ${metric(kpis?.p99Ms, " ms")}`],
    ["入站 / 出站", `${formatBytes(toNumber(kpis?.inboundBytes))} / ${formatBytes(toNumber(kpis?.outboundBytes))}`],
    ["平均 / 最大耗时", `${metric(kpis?.avgDurationMs, " ms")} / ${metric(kpis?.maxDurationMs, " ms")}`]
  ];
  return <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">{cards.map(([title, value]) => <Card key={title}><CardContent className="p-4"><Statistic title={title} value={value} /></CardContent></Card>)}</div>;
};

export const ResourceTrend: React.FC<{ overview?: IAnalyticsOverview; timeseries?: IAnalyticsTimeseries; loading?: boolean; aside?: React.ReactNode; height?: number }> = ({ overview, timeseries, loading, aside, height = 320 }) => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const points = timeseries?.points ?? [];
  const option = buildAnalyticsTrendOption(points, colors);
  return <div className="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-3">
    <div className="xl:col-span-2"><EChartPanel title="请求与错误趋势" loading={loading} isEmpty={!points.length} option={points.length ? option : undefined} height={height} emptyDescription="当前窗口暂无观测数据" /></div>
    {aside ?? <MonCard title="错误分布" loading={loading} className="h-[320px] overflow-auto">
      {(overview?.errorBreakdown ?? []).length
        ? <div className="flex flex-wrap gap-2">{overview?.errorBreakdown.map((item) => <Badge key={item.type} variant="warning">{item.type}: {formatNumber(item.value)}</Badge>)}</div>
        : <span className="text-sm text-muted-foreground">当前窗口没有错误记录</span>}
    </MonCard>}
  </div>;
};
