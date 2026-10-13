import EChartPanel from "@/componets/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/componets/charts/analyticsTrend";
import { IAnalyticsOverview, IAnalyticsTimeseries } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import { formatBytes, formatNumber, formatPercent, toNumber } from "@/utils/formatters";
import { Card, Col, Row, Statistic, Tag } from "antd";
import { SPACE } from "@/utils/spacing";
import React from "react";

export const MetricCards: React.FC<{ overview?: IAnalyticsOverview }> = ({ overview }) => {
  const kpis = overview?.kpis;
  const cards = [
    ["请求数", formatNumber(toNumber(kpis?.requests))], ["吞吐", `${formatNumber(toNumber(kpis?.requestsPerSecond))} req/s`],
    ["出站流量", formatBytes(toNumber(kpis?.outboundBytes))], ["P95", `${formatNumber(toNumber(kpis?.p95Ms))} ms`],
    ["错误率", formatPercent(toNumber(kpis?.errorRate))], ["最长耗时", `${formatNumber(toNumber(kpis?.maxDurationMs))} ms`]
  ];
  return <Row gutter={[SPACE.space3, SPACE.space3]}>{cards.map(([title, value]) => <Col key={title} xs={12} md={8} xl={4}><Card size="small"><Statistic title={title} value={value} /></Card></Col>)}</Row>;
};

export const ResourceTrend: React.FC<{ overview?: IAnalyticsOverview; timeseries?: IAnalyticsTimeseries; loading?: boolean }> = ({ overview, timeseries, loading }) => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const points = timeseries?.points ?? [];
  const option = buildAnalyticsTrendOption(points, colors);
  return <Row gutter={[SPACE.space3, SPACE.space3]} style={{ marginTop: "var(--space-3)" }}>
    <Col xs={24} xl={16}><EChartPanel title="请求与错误趋势" loading={loading} isEmpty={!points.length} option={points.length ? option : undefined} height={320} emptyDescription="当前窗口暂无观测数据" /></Col>
    <Col xs={24} xl={8}><Card title="错误分布" loading={loading} style={{ height: 320 }}>{(overview?.errorBreakdown ?? []).length ? overview?.errorBreakdown.map((item) => <Tag key={item.type} color="volcano" style={{ marginBottom: "var(--space-2)" }}>{item.type}: {formatNumber(item.value)}</Tag>) : "当前窗口没有错误记录"}</Card></Col>
  </Row>;
};
