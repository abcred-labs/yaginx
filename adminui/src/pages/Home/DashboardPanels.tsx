import { getAnalyticsOverview, getDatabasePerformance, getGatewayDiagnosticsOverview, overviewSection, type IAnalyticsModeMetrics, type IAnalyticsOverview, type IAnalyticsPoint, type IDatabasePerformance, type IGatewayDiagnosticsOverview } from "@/api/analytics";
import EChartPanel from "@/componets/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/componets/charts/analyticsTrend";
import { formatBytes, formatNumber, formatPercent } from "@/utils/formatters";
import { Alert, Badge, Button, Card, Col, Empty, Progress, Row, Space, Statistic, Tag, Typography } from "antd";
import React, { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { SPACE } from "@/utils/spacing";

export interface DashboardPanelProps { range: number; selectedDomain?: string; refreshKey?: number; autoRefreshSeconds?: number; onLoadingChange?: (loading: boolean) => void; }
type FetchState<T> = { data?: T; loading: boolean; error?: string; stale?: boolean };

const useIntervalRefresh = (refreshKey: number | undefined, intervalMs: number | undefined, load: () => void) => {
  useEffect(() => {
    load();
    if (!intervalMs) return;
    const timer = window.setInterval(load, intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs, load, refreshKey]);
};

const useAnalytics = (props: DashboardPanelProps, sections: ("overview" | "domains" | "timeseries")[]) => {
  const [state, setState] = useState<FetchState<IAnalyticsOverview>>({ loading: true });
  const controllerRef = React.useRef<AbortController | undefined>(undefined);
  const load = React.useCallback(() => {
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    const to = Math.floor(Date.now() / 1000);
    props.onLoadingChange?.(true);
    setState((current) => ({ ...current, loading: !current.data, error: undefined }));
    getAnalyticsOverview({ from: to - props.range, to, domain: props.selectedDomain }, sections, { ignoreGlobalError: true, signal: controller.signal })
      .then((response) => { if (controller.signal.aborted) return; const data = response.data ? overviewSection(response.data) : undefined; if (response.code === 200 && data) setState({ data, loading: false }); else setState((current) => ({ ...current, loading: false, error: "指标加载失败" })); })
      .catch(() => { if (!controller.signal.aborted) setState((current) => ({ ...current, loading: false, error: "指标加载失败，请检查网络连接" })); })
      .finally(() => { if (!controller.signal.aborted) props.onLoadingChange?.(false); });
  }, [props.range, props.selectedDomain, props.onLoadingChange, JSON.stringify(sections)]);
  useEffect(() => {
    load();
    if (!props.autoRefreshSeconds) return;
    const timer = window.setInterval(load, props.autoRefreshSeconds * 1000);
    return () => { window.clearInterval(timer); controllerRef.current?.abort(); };
  }, [load, props.autoRefreshSeconds, props.refreshKey]);
  return { ...state, reload: load };
};

const cardError = (error?: string) => error ? <Alert type="error" showIcon message={error} /> : null;

export const TrafficKpiPanel: React.FC<DashboardPanelProps> = (props) => {
  const { data, loading, error } = useAnalytics(props, ["overview"]);
  const kpis = data?.kpis;
  const cards = [
    ["请求总计", formatNumber(kpis?.requests)], ["当前吞吐", `${formatNumber(kpis?.requestsPerSecond)} req/s`], ["错误率", formatPercent(kpis?.errorRate)],
    ["P95 / P99", `${kpis?.p95Ms == null ? "-" : formatNumber(kpis.p95Ms)} / ${kpis?.p99Ms == null ? "-" : formatNumber(kpis.p99Ms)} ms`],
    ["当前并发", formatNumber(data?.currentInFlight)], ["活跃 Host", formatNumber(data?.activeHostCount)],
    ["入站流量", formatBytes(kpis?.inboundBytes)], ["出站流量", formatBytes(kpis?.outboundBytes)]
  ];
  const modes = data?.modes;
  const modeCard = (title: string, mode: IAnalyticsModeMetrics) => <Col xs={24} lg={8}><Card size="small" title={title}><Row gutter={[SPACE.space2, SPACE.space2]}><Col span={12}><Statistic title="请求数" value={formatNumber(mode.requests)} /></Col><Col span={12}><Statistic title="完成 P95" value={mode.completionLatency.p95Ms == null ? "-" : `${formatNumber(mode.completionLatency.p95Ms)} ms`} /></Col><Col span={12}><Statistic title="首字节 P95" value={mode.firstByteLatency.p95Ms == null ? "-" : `${formatNumber(mode.firstByteLatency.p95Ms)} ms`} /></Col><Col span={12}><Statistic title="首 token P95" value={mode.firstTokenLatency.p95Ms == null ? "-" : `${formatNumber(mode.firstTokenLatency.p95Ms)} ms`} /></Col></Row></Card></Col>;
  return <Card title="流量信号" extra={<Typography.Text type="secondary">窗口内汇总</Typography.Text>} loading={loading}>{cardError(error)}<Row gutter={[SPACE.space3, SPACE.space3]}>{cards.map(([title, value]) => <Col key={title} xs={12} sm={6} xl={3}><Statistic title={title} value={value} /></Col>)}</Row>{modes && <Row gutter={[SPACE.space3, SPACE.space3]} style={{ marginTop: SPACE.space3 }}>{modeCard("非流式请求", modes.nonStream)}{modeCard("流式请求", modes.stream)}{modeCard("升级连接", modes.upgrade)}</Row>}</Card>;
};

const trendColors = { flow: "#2563eb", alert: "#dc5a42", grid: "#dbe4ef", neutral: "#718198", text: "#17233d" };

export const buildTrendWithLatency = (points: IAnalyticsPoint[]) => {
  const base = buildAnalyticsTrendOption(points, trendColors) as any;
  const labels = points.map(point => new Date(point.bucketStartAt * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
  const series = Array.isArray(base.series) ? base.series : [];
  return {
    ...base,
    legend: { ...(base.legend ?? {}), data: ["Requests/s", "Errors", "P50", "P95", "P99"] },
    yAxis: [...(Array.isArray(base.yAxis) ? base.yAxis : []), { type: "value", name: "Latency", axisLabel: { color: trendColors.neutral, formatter: (value: number) => `${value} ms` }, splitLine: { show: false } }],
    series: [...series, ...[
      ["P50", "p50Ms", "#5b8def"], ["P95", "p95Ms", "#f59e0b"], ["P99", "p99Ms", "#dc5a42"]
    ].map(([name, field, color]) => ({ name, type: "line", yAxisIndex: 2, smooth: true, symbol: "none", lineStyle: { width: name === "P95" ? 2 : 1, color }, data: points.map(point => point[field as "p50Ms" | "p95Ms" | "p99Ms"]) }))],
    xAxis: { ...(base.xAxis ?? {}), data: labels }
  };
};

export const TrafficTrendPanel: React.FC<DashboardPanelProps> = (props) => {
  const { data, loading, error } = useAnalytics(props, ["overview", "timeseries"]);
  const points = (data as IAnalyticsOverview & { points?: IAnalyticsPoint[] } | undefined)?.points ?? [];
  return <EChartPanel title="请求与延迟趋势" loading={loading} isEmpty={!points.length} option={points.length ? buildTrendWithLatency(points) : undefined} height={330} emptyDescription={error || "当前窗口暂无观测数据"} hint={data?.isPartial ? "末个统计桶仍在收集" : undefined} />;
};

export const TrafficBreakdownPanel: React.FC<DashboardPanelProps> = (props) => {
  const navigate = useNavigate();
  const { data, loading, error } = useAnalytics(props, ["overview", "domains"]);
  const status = data?.statusBreakdown ?? [];
  const errors = data?.errorBreakdown ?? [];
  const domains = data?.topDomains ?? [];
  const openStatusHistory = (family: string) => {
    if (!data || !/^[1-6]xx$/i.test(family)) return;
    const query = new URLSearchParams({
      statusCodePattern: family,
      from: String(data.from),
      to: String(data.to),
    });
    if (props.selectedDomain) query.set("host", props.selectedDomain);
    navigate(`/monitor/requestHistory/list?${query.toString()}`);
  };
  return <Card title="请求分布与异常" loading={loading}>{cardError(error)}<Row gutter={[SPACE.space4, SPACE.space4]}>
    <Col xs={24} md={8}><Typography.Text strong>状态码分布</Typography.Text><div className="dashboard-chip-list">{status.length ? status.map(item => {
      const isStatusFamily = /^[1-6]xx$/i.test(item.type);
      return <Tag key={item.type} color={item.type.startsWith("5") || item.type.startsWith("6") ? "red" : "blue"} style={{ cursor: isStatusFamily ? "pointer" : "default" }} onClick={isStatusFamily ? () => openStatusHistory(item.type) : undefined}>{item.type} · {formatNumber(item.value)}</Tag>;
    }) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无状态码" />}</div></Col>
    <Col xs={24} md={8}><Typography.Text strong>错误分布</Typography.Text><div className="dashboard-chip-list">{errors.length ? errors.map(item => <Tag key={item.type} color="volcano" onClick={() => navigate(`/monitor/requestHistory/list?errorCode=${item.type}`)}>{item.type} · {formatNumber(item.value)}</Tag>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前窗口无错误" />}</div></Col>
    <Col xs={24} md={8}><Typography.Text strong>Top 域名</Typography.Text>{domains.slice(0, 5).map(domain => <Button key={domain.domain} type="link" block className="dashboard-domain-row" onClick={() => navigate(`/monitor/traffic/request-trend?domain=${encodeURIComponent(domain.domain)}`)}><span>{domain.domain}</span><span>{formatNumber(domain.requests)}</span></Button>)}</Col>
  </Row></Card>;
};

export const GatewayHealthPanel: React.FC = () => {
  const [state, setState] = useState<FetchState<IGatewayDiagnosticsOverview>>({ loading: true });
  const load = React.useCallback(() => {
    const controller = new AbortController();
    setState(current => ({ ...current, loading: !current.data }));
    getGatewayDiagnosticsOverview({ ignoreGlobalError: true, signal: controller.signal }).then(response => { if (controller.signal.aborted) return; if (response.code === 200 && response.data) setState({ data: response.data, loading: false, stale: false }); else setState(current => ({ ...current, loading: false, stale: true, error: "网关运行态加载失败" })); }).catch(() => { if (!controller.signal.aborted) setState(current => ({ ...current, loading: false, stale: true, error: "网关运行态加载失败" })); });
    return () => controller.abort();
  }, []);
  useIntervalRefresh(undefined, 15000, load);
  const data = state.data;
  const queue = Object.values(data?.runtime.queues ?? {}).reduce((sum, item) => sum + item.depth, 0);
  return <Card title="网关健康" loading={state.loading} extra={data ? <Typography.Text type={state.stale ? "warning" : "secondary"}>{state.stale ? "数据已过期" : `更新于 ${new Date(data.asOf * 1000).toLocaleTimeString()}`}</Typography.Text> : null}>{cardError(state.error)}<Space wrap className="dashboard-health-status"><Badge status={data?.health.level === "healthy" ? "success" : data?.health.level === "critical" ? "error" : "warning"} text={data?.health.level === "healthy" ? "健康" : "需要关注"} />{data?.health.reasons?.map(reason => <Tag key={reason} color="gold">{reason}</Tag>)}</Space><Row gutter={[SPACE.space3, SPACE.space3]} className="dashboard-health-grid"><Col xs={12} sm={8}><Statistic title="节点" value={`${data?.health.activeNodes ?? 0}/${data?.health.totalNodes ?? 0}`} suffix="活跃" /></Col><Col xs={12} sm={8}><Statistic title="进程 RSS" value={formatBytes(data?.runtime.residentBytes)} /></Col><Col xs={12} sm={8}><Statistic title="Heap" value={formatBytes(data?.runtime.heapAllocBytes)} /></Col><Col xs={12} sm={8}><Statistic title="Goroutine" value={formatNumber(data?.runtime.goroutines)} /></Col><Col xs={12} sm={8}><Statistic title="队列深度" value={formatNumber(queue)} /></Col><Col xs={12} sm={8}><Statistic title="当前并发" value={formatNumber(data?.runtime.activeRequests)} /></Col></Row></Card>;
};

export const DatabaseHealthPanel: React.FC = () => {
  const navigate = useNavigate();
  const [state, setState] = useState<FetchState<IDatabasePerformance>>({ loading: true });
  const load = React.useCallback(() => {
    const controller = new AbortController();
    setState(current => ({ ...current, loading: !current.data }));
    getDatabasePerformance({ ignoreGlobalError: true, signal: controller.signal }).then(response => { if (controller.signal.aborted) return; if (response.code === 200 && response.data) setState({ data: response.data, loading: false }); else setState(current => ({ ...current, loading: false, stale: true, error: "数据库性能加载失败" })); }).catch(() => { if (!controller.signal.aborted) setState(current => ({ ...current, loading: false, stale: true, error: "数据库性能加载失败" })); });
    return () => controller.abort();
  }, []);
  useIntervalRefresh(undefined, 15000, load);
  const data = state.data;
  const health = data?.health.level ?? "healthy";
  return <Card title="数据库健康" loading={state.loading} extra={<Button type="link" onClick={() => navigate("/systems/database")}>查看详情</Button>}>{cardError(state.error)}<Space wrap className="dashboard-health-status"><Badge status={health === "healthy" ? "success" : health === "critical" ? "error" : "warning"} text={health === "healthy" ? "健康" : health === "critical" ? "严重" : "注意"} />{data?.health.reasons?.map(reason => <Tag key={reason} color="gold">{reason}</Tag>)}</Space><Progress percent={Math.round((data?.pool.usageRate ?? 0) * 100)} status={(data?.pool.usageRate ?? 0) >= .8 ? "exception" : "active"} format={value => `连接池 ${value}%`} /><Row gutter={[SPACE.space3, SPACE.space3]}><Col span={12}><Statistic title="锁等待" value={formatNumber(data?.activity.lockWaits)} /></Col><Col span={12}><Statistic title="缓存命中率" value={`${Math.round((data?.database.cacheHitRate ?? 0) * 100)}%`} /></Col></Row></Card>;
};
