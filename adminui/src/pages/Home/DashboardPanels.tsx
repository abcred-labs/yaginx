import { getAnalyticsOverview, getDatabasePerformance, getGatewayDiagnosticsOverview, overviewSection, type IAnalyticsModeMetrics, type IAnalyticsOverview, type IAnalyticsPoint, type IDatabasePerformance, type IGatewayDiagnosticsOverview } from "@/api/analytics";
import EChartPanel from "@/components/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/components/charts/analyticsTrend";
import { formatBytes, formatNumber, formatPercent } from "@/utils/formatters";
import { Badge, Button, Empty, Progress } from "@modra/react";
import React, { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { hostDetailPath } from "@/utils/analyticsLinks";
import { MonAlert, MonCard, MonStat, cx } from "@/ui/monParts";

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

const cardError = (error?: string) => error ? <MonAlert type="error" message={error} className="mb-3" /> : null;
const chipList = "mt-3 flex min-h-[76px] flex-wrap content-start gap-2";
const subLabel = "text-sm font-semibold";

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
  const modeCard = (title: string, mode: IAnalyticsModeMetrics) => <MonCard title={title}><div className="grid grid-cols-2 gap-2"><MonStat title="请求数" value={formatNumber(mode.requests)} /><MonStat title="完成 P95" value={mode.completionLatency.p95Ms == null ? "-" : `${formatNumber(mode.completionLatency.p95Ms)} ms`} /><MonStat title="首字节 P95" value={mode.firstByteLatency.p95Ms == null ? "-" : `${formatNumber(mode.firstByteLatency.p95Ms)} ms`} /><MonStat title="首 token P95" value={mode.firstTokenLatency.p95Ms == null ? "-" : `${formatNumber(mode.firstTokenLatency.p95Ms)} ms`} /></div></MonCard>;
  return <MonCard title="流量信号" extra={<span className="text-sm text-muted-foreground">窗口内汇总</span>} loading={loading}>{cardError(error)}<div className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-8">{cards.map(([title, value]) => <MonStat key={title} title={title} value={value} />)}</div>{modes && <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-3">{modeCard("非流式请求", modes.nonStream)}{modeCard("流式请求", modes.stream)}{modeCard("升级连接", modes.upgrade)}</div>}</MonCard>;
};

const trendColors = { flow: "#2563eb", alert: "#dc5a42", grid: "#dbe4ef", neutral: "#718198", text: "#17233d" };

// Errors 轴已经占用右侧，Latency 轴再向外偏移一个轴宽，否则两列刻度会叠在一起。
const LATENCY_AXIS_OFFSET = 64;

export const buildTrendWithLatency = (points: IAnalyticsPoint[]) => {
  const base = buildAnalyticsTrendOption(points, trendColors) as any;
  const labels = points.map(point => new Date(point.bucketStartAt * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
  const series = Array.isArray(base.series) ? base.series : [];
  return {
    ...base,
    legend: { ...(base.legend ?? {}), data: ["Requests/s", "Errors", "P50", "P95", "P99"] },
    grid: { ...(base.grid ?? {}), right: (base.grid?.right ?? 28) + LATENCY_AXIS_OFFSET },
    yAxis: [...(Array.isArray(base.yAxis) ? base.yAxis : []), { type: "value", name: "Latency", position: "right", offset: LATENCY_AXIS_OFFSET, axisLabel: { color: trendColors.neutral, formatter: (value: number) => `${value} ms` }, splitLine: { show: false } }],
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
  return <MonCard title="请求分布与异常" loading={loading}>{cardError(error)}<div className="grid grid-cols-1 gap-[14px] md:grid-cols-3">
    <div><span className={subLabel}>状态码分布</span><div className={chipList}>{status.length ? status.map(item => {
      const isStatusFamily = /^[1-6]xx$/i.test(item.type);
      const content = `${item.type} · ${formatNumber(item.value)}`;
      const variant = item.type.startsWith("5") || item.type.startsWith("6") ? "destructive" : "info";
      return isStatusFamily
        ? <button key={item.type} type="button" onClick={() => openStatusHistory(item.type)}><Badge variant={variant} className="cursor-pointer">{content}</Badge></button>
        : <Badge key={item.type} variant={variant}>{content}</Badge>;
    }) : <Empty description="暂无状态码" />}</div></div>
    <div><span className={subLabel}>错误分布</span><div className={chipList}>{errors.length ? errors.map(item => <button key={item.type} type="button" onClick={() => navigate(`/monitor/requestHistory/list?errorCode=${item.type}`)}><Badge variant="warning" className="cursor-pointer">{item.type} · {formatNumber(item.value)}</Badge></button>) : <Empty description="当前窗口无错误" />}</div></div>
    <div><span className={subLabel}>Top 域名</span>{domains.slice(0, 5).map(domain => <button key={domain.domain} type="button" className="flex w-full items-center justify-between border-b border-border py-1.5 text-left text-sm text-primary hover:underline" onClick={() => navigate(hostDetailPath(domain.domain))}><span>{domain.domain}</span><span className="font-mono text-muted-foreground">{formatNumber(domain.requests)}</span></button>)}</div>
  </div></MonCard>;
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
  return <MonCard title="网关健康" loading={state.loading} extra={data ? <span className={cx("text-sm", state.stale ? "text-warning" : "text-muted-foreground")}>{state.stale ? "数据已过期" : `更新于 ${new Date(data.asOf * 1000).toLocaleTimeString()}`}</span> : null}>{cardError(state.error)}<div className="mb-3 flex flex-wrap items-center gap-2"><Badge variant={data?.health.level === "healthy" ? "success" : data?.health.level === "critical" ? "destructive" : "warning"}>{data?.health.level === "healthy" ? "健康" : "需要关注"}</Badge>{data?.health.reasons?.map(reason => <Badge key={reason} variant="warning">{reason}</Badge>)}</div><div className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-3"><MonStat title="节点" value={`${data?.health.activeNodes ?? 0}/${data?.health.totalNodes ?? 0}`} suffix="活跃" /><MonStat title="进程 RSS" value={formatBytes(data?.runtime.residentBytes)} /><MonStat title="Heap" value={formatBytes(data?.runtime.heapAllocBytes)} /><MonStat title="Goroutine" value={formatNumber(data?.runtime.goroutines)} /><MonStat title="队列深度" value={formatNumber(queue)} /><MonStat title="当前并发" value={formatNumber(data?.runtime.activeRequests)} /></div></MonCard>;
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
  const poolPercent = Math.round((data?.pool.usageRate ?? 0) * 100);
  return <MonCard title="数据库健康" loading={state.loading} extra={<Button variant="link" size="sm" onClick={() => navigate("/systems/database")}>查看详情</Button>}>{cardError(state.error)}<div className="mb-3 flex flex-wrap items-center gap-2"><Badge variant={health === "healthy" ? "success" : health === "critical" ? "destructive" : "warning"}>{health === "healthy" ? "健康" : health === "critical" ? "严重" : "注意"}</Badge>{data?.health.reasons?.map(reason => <Badge key={reason} variant="warning">{reason}</Badge>)}</div><div className="mb-3"><div className="mb-1 text-sm">连接池 {poolPercent}%</div><Progress value={poolPercent} className={(data?.pool.usageRate ?? 0) >= .8 ? "[&>div]:bg-destructive" : undefined} /></div><div className="grid grid-cols-2 gap-3"><MonStat title="锁等待" value={formatNumber(data?.activity.lockWaits)} /><MonStat title="缓存命中率" value={`${Math.round((data?.database.cacheHitRate ?? 0) * 100)}%`} /></div></MonCard>;
};
