import EChartPanel from "@/components/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/components/charts/analyticsTrend";
import { IAnalyticsOverview, IAnalyticsTimeseries } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import { formatBytes, formatNumber, formatPercent, toNumber } from "@/utils/formatters";
import { Badge, Card, CardContent, Statistic } from "@modra/react";
import React from "react";
import { MonCard } from "@/ui/monParts";

export const MetricCards: React.FC<{ overview?: IAnalyticsOverview }> = ({ overview }) => {
  const kpis = overview?.kpis;
  const cards = [
    ["请求数", formatNumber(toNumber(kpis?.requests))], ["吞吐", `${formatNumber(toNumber(kpis?.requestsPerSecond))} req/s`],
    ["出站流量", formatBytes(toNumber(kpis?.outboundBytes))], ["P95", `${formatNumber(toNumber(kpis?.p95Ms))} ms`],
    ["错误率", formatPercent(toNumber(kpis?.errorRate))], ["最长耗时", `${formatNumber(toNumber(kpis?.maxDurationMs))} ms`]
  ];
  return <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">{cards.map(([title, value]) => <Card key={title}><CardContent className="p-4"><Statistic title={title} value={value} /></CardContent></Card>)}</div>;
};

export const ResourceTrend: React.FC<{ overview?: IAnalyticsOverview; timeseries?: IAnalyticsTimeseries; loading?: boolean }> = ({ overview, timeseries, loading }) => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const points = timeseries?.points ?? [];
  const option = buildAnalyticsTrendOption(points, colors);
  return <div className="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-3">
    <div className="xl:col-span-2"><EChartPanel title="请求与错误趋势" loading={loading} isEmpty={!points.length} option={points.length ? option : undefined} height={320} emptyDescription="当前窗口暂无观测数据" /></div>
    <MonCard title="错误分布" loading={loading} className="h-[320px] overflow-auto">
      {(overview?.errorBreakdown ?? []).length
        ? <div className="flex flex-wrap gap-2">{overview?.errorBreakdown.map((item) => <Badge key={item.type} variant="warning">{item.type}: {formatNumber(item.value)}</Badge>)}</div>
        : <span className="text-sm text-muted-foreground">当前窗口没有错误记录</span>}
    </MonCard>
  </div>;
};
